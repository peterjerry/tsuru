// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kubernetes

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/auth"
	"github.com/tsuru/tsuru/auth/native"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/dbtest"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/quota"
	"github.com/tsuru/tsuru/router/routertest"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/check.v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	batch "k8s.io/client-go/pkg/apis/batch/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

type S struct {
	p        *kubernetesProvisioner
	conn     *db.Storage
	user     *auth.User
	team     *auth.Team
	token    auth.Token
	client   *clientWrapper
	lastConf *rest.Config
	t        *testing.T
}

var suiteInstance = &S{}
var _ = check.Suite(suiteInstance)

func Test(t *testing.T) {
	suiteInstance.t = t
	check.TestingT(t)
}

func (s *S) SetUpSuite(c *check.C) {
	config.Set("auth:hash-cost", bcrypt.MinCost)
	config.Set("database:url", "127.0.0.1:27017")
	config.Set("kubernetes:token", "token==")
	config.Set("database:name", "provision_kubernetes_tests_s")
	config.Set("routers:fake:type", "fake")
	config.Set("routers:fake:default", true)
	var err error
	s.conn, err = db.Conn()
	c.Assert(err, check.IsNil)
}

func (s *S) TearDownSuite(c *check.C) {
	s.conn.Close()
}

type clientWrapper struct {
	*fake.Clientset
}

func (c *clientWrapper) Core() v1core.CoreV1Interface {
	core := c.Clientset.Core()
	return &clientCoreWrapper{core}
}

type clientCoreWrapper struct {
	v1core.CoreV1Interface
}

func (c *clientCoreWrapper) Pods(namespace string) v1core.PodInterface {
	pods := c.CoreV1Interface.Pods(namespace)
	return &clientPodsWrapper{pods}
}

type clientPodsWrapper struct {
	v1core.PodInterface
}

func (c *clientPodsWrapper) GetLogs(name string, opts *v1.PodLogOptions) *rest.Request {
	c.PodInterface.GetLogs(name, opts)
	cfg, _ := getClusterRestConfig()
	cli, _ := rest.RESTClientFor(cfg)
	return cli.Get().Namespace(tsuruNamespace).Name(name).Resource("pods").SubResource("log").VersionedParams(opts, api.ParameterCodec)
}

func (s *S) SetUpTest(c *check.C) {
	s.client = &clientWrapper{fake.NewSimpleClientset()}
	clientForConfig = func(conf *rest.Config) (kubernetes.Interface, error) {
		s.lastConf = conf
		return s.client, nil
	}
	routertest.FakeRouter.Reset()
	rand.Seed(0)
	err := dbtest.ClearAllCollections(s.conn.Apps().Database)
	c.Assert(err, check.IsNil)
	err = provision.AddPool(provision.AddPoolOptions{
		Name:        "bonehunters",
		Default:     true,
		Provisioner: "kubernetes",
	})
	c.Assert(err, check.IsNil)
	p := app.Plan{
		Name:     "default",
		Default:  true,
		CpuShare: 100,
	}
	err = p.Save()
	c.Assert(err, check.IsNil)
	s.p = &kubernetesProvisioner{}
	s.user = &auth.User{Email: "whiskeyjack@genabackis.com", Password: "123456", Quota: quota.Unlimited}
	nativeScheme := auth.ManagedScheme(native.NativeScheme{})
	app.AuthScheme = nativeScheme
	_, err = nativeScheme.Create(s.user)
	c.Assert(err, check.IsNil)
	s.team = &auth.Team{Name: "admin"}
	c.Assert(err, check.IsNil)
	err = s.conn.Teams().Insert(s.team)
	c.Assert(err, check.IsNil)
	s.token, err = nativeScheme.Login(map[string]string{"email": s.user.Email, "password": "123456"})
	c.Assert(err, check.IsNil)
}

func (s *S) mockfakeNodes(c *check.C, urls ...string) {
	url := "https://anything"
	if len(urls) > 0 {
		url = urls[0]
	}
	opts := provision.AddNodeOptions{
		Address: url,
		Metadata: map[string]string{
			"cluster": "true",
		},
	}
	err := s.p.AddNode(opts)
	c.Assert(err, check.IsNil)
	for i := 1; i <= 2; i++ {
		_, err = s.client.Core().Nodes().Create(&v1.Node{
			ObjectMeta: v1.ObjectMeta{
				Name: fmt.Sprintf("n%d", i),
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: fmt.Sprintf("192.168.99.%d", i),
					},
					{
						Type:    v1.NodeExternalIP,
						Address: fmt.Sprintf("200.0.0.%d", i),
					},
				},
			},
		})
		c.Assert(err, check.IsNil)
	}
}

func (s *S) createDeployReadyServer(c *check.C) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/attach") {
			_, streamErr := httpstream.Handshake(r, w, []string{"v4.channel.k8s.io"})
			c.Assert(streamErr, check.IsNil)
			upgrader := spdy.NewResponseUpgrader()
			streams := make(chan httpstream.Stream, 4)
			upgrader.UpgradeResponse(w, r, func(stream httpstream.Stream, replySent <-chan struct{}) error {
				streams <- stream
				return nil
			})
			for stream := range streams {
				stream.Close()
			}
		} else if strings.HasSuffix(r.URL.Path, "/logs") {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "my app log")
		}
	}))
	return srv
}

func (s *S) jobWithPodReaction(a provision.App, c *check.C) ktesting.ReactionFunc {
	return func(action ktesting.Action) (bool, runtime.Object, error) {
		job := action.(ktesting.CreateAction).GetObject().(*batch.Job)
		job.Status.Succeeded = int32(1)
		go func() {
			pod := &v1.Pod{
				ObjectMeta: job.Spec.Template.ObjectMeta,
				Spec:       job.Spec.Template.Spec,
			}
			pod.ObjectMeta.Namespace = job.Namespace
			pod.ObjectMeta.Labels["job-name"] = job.Name
			toRegister := false
			for _, cont := range pod.Spec.Containers {
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, v1.ContainerStatus{
					Name: cont.Name,
					State: v1.ContainerState{
						Running: &v1.ContainerStateRunning{},
					},
				})
				if strings.Contains(strings.Join(cont.Command, " "), "unit_agent") {
					toRegister = true
				}
			}
			_, err := s.client.Core().Pods(job.Namespace).Create(pod)
			c.Assert(err, check.IsNil)
			if toRegister {
				err = s.p.RegisterUnit(a, job.Name, map[string]interface{}{
					"processes": map[string]interface{}{
						"web":    "python myapp.py",
						"worker": "python myworker.py",
					},
				})
				c.Assert(err, check.IsNil)
			}
		}()
		return false, nil, nil
	}
}
