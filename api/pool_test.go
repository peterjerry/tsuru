// Copyright 2015 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/tsuru/tsuru/auth"
	"github.com/tsuru/tsuru/event"
	"github.com/tsuru/tsuru/event/eventtest"
	"github.com/tsuru/tsuru/permission"
	"github.com/tsuru/tsuru/provision"
	"gopkg.in/check.v1"
	"gopkg.in/mgo.v2/bson"
)

func (s *S) TestAddPoolNameIsRequired(c *check.C) {
	b := bytes.NewBufferString("name=")
	request, err := http.NewRequest("POST", "/pools", b)
	c.Assert(err, check.IsNil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "bearer "+s.token.GetValue())
	recorder := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(recorder, request)
	c.Assert(recorder.Code, check.Equals, http.StatusBadRequest)
	c.Assert(recorder.Body.String(), check.Equals, provision.ErrPoolNameIsRequired.Error()+"\n")
}

func (s *S) TestAddPoolDefaultPoolAlreadyExists(c *check.C) {
	b := bytes.NewBufferString("name=pool1&default=true")
	req, err := http.NewRequest("POST", "/pools", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	defer provision.RemovePool("pool1")
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusConflict)
	c.Assert(rec.Body.String(), check.Equals, provision.ErrDefaultPoolAlreadyExists.Error()+"\n")
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.create",
		StartCustomData: []map[string]interface{}{
			{"name": "name", "value": "pool1"},
			{"name": "default", "value": "true"},
		},
		ErrorMatches: `Default pool already exists\.`,
	}, eventtest.HasEvent)
}

func (s *S) TestAddPool(c *check.C) {
	b := bytes.NewBufferString("name=pool1")
	req, err := http.NewRequest("POST", "/pools", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	defer provision.RemovePool("pool1")
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusCreated)
	c.Assert(err, check.IsNil)
	_, err = provision.GetPoolByName("pool1")
	c.Assert(err, check.IsNil)
	b = bytes.NewBufferString("name=pool2&public=true")
	req, err = http.NewRequest("POST", "/pools", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	defer provision.RemovePool("pool2")
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusCreated)
	pool, err := provision.GetPoolByName("pool2")
	c.Assert(err, check.IsNil)
	teams, err := pool.GetTeams()
	c.Assert(err, check.IsNil)
	c.Assert(teams, check.DeepEquals, []string{"tsuruteam"})
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.create",
		StartCustomData: []map[string]interface{}{
			{"name": "name", "value": "pool1"},
		},
	}, eventtest.HasEvent)
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool2"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.create",
		StartCustomData: []map[string]interface{}{
			{"name": "name", "value": "pool2"},
			{"name": "public", "value": "true"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestRemovePoolNotFound(c *check.C) {
	req, err := http.NewRequest("DELETE", "/pools/not-found", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusNotFound)
}

func (s *S) TestRemovePoolHandler(c *check.C) {
	opts := provision.AddPoolOptions{
		Name: "pool1",
	}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	req, err := http.NewRequest("DELETE", "/pools/pool1", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	_, err = provision.GetPoolByName("pool1")
	c.Assert(err, check.Equals, provision.ErrPoolNotFound)
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.delete",
		StartCustomData: []map[string]interface{}{
			{"name": ":name", "value": "pool1"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestAddTeamsToPoolWithoutTeam(c *check.C) {
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	b := strings.NewReader("")
	req, err := http.NewRequest("POST", "/pools/pool1/team", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusBadRequest)
}

func (s *S) TestAddTeamsToPool(c *check.C) {
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	b := strings.NewReader("team=tsuruteam")
	req, err := http.NewRequest("POST", "/pools/pool1/team", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	p, err := provision.GetPoolByName("pool1")
	c.Assert(err, check.IsNil)
	teams, err := p.GetTeams()
	c.Assert(err, check.IsNil)
	c.Assert(teams, check.DeepEquals, []string{"tsuruteam"})
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.update.team.add",
		StartCustomData: []map[string]interface{}{
			{"name": ":name", "value": "pool1"},
			{"name": "team", "value": "tsuruteam"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestAddTeamsToPoolNotFound(c *check.C) {
	b := strings.NewReader("team=test")
	req, err := http.NewRequest("POST", "/pools/notfound/team", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusNotFound)
}

func (s *S) TestRemoveTeamsToPoolNotFound(c *check.C) {
	req, err := http.NewRequest("DELETE", "/pools/not-found/team?team=team", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusNotFound)
}

func (s *S) TestRemoveTeamsToPoolWithoutTeam(c *check.C) {
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool(pool.Name, []string{"test"})
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	req, err := http.NewRequest("DELETE", "/pools/pool1/team", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusBadRequest)
}

func (s *S) TestRemoveTeamsToPoolHandler(c *check.C) {
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool(pool.Name, []string{"tsuruteam"})
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	req, err := http.NewRequest("DELETE", "/pools/pool1/team?team=tsuruteam", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	var p provision.Pool
	err = s.conn.Pools().FindId(pool.Name).One(&p)
	c.Assert(err, check.IsNil)
	_, err = p.GetTeams()
	c.Assert(err, check.NotNil)
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.update.team.remove",
		StartCustomData: []map[string]interface{}{
			{"name": ":name", "value": "pool1"},
			{"name": "team", "value": "tsuruteam"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestPoolListPublicPool(c *check.C) {
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name, Public: true}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	defaultPool, err := provision.GetDefaultPool()
	c.Assert(err, check.IsNil)
	expected := []provision.Pool{
		*defaultPool,
		{Name: "pool1"},
	}
	token := userWithPermission(c, permission.Permission{
		Scheme:  permission.PermTeamCreate,
		Context: permission.Context(permission.CtxGlobal, ""),
	})
	req, err := http.NewRequest("GET", "/pools", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	err = poolList(rec, req, token)
	c.Assert(err, check.IsNil)
	var pools []provision.Pool
	err = json.NewDecoder(rec.Body).Decode(&pools)
	c.Assert(err, check.IsNil)
	c.Assert(pools, check.DeepEquals, expected)
}

func (s *S) TestPoolListHandler(c *check.C) {
	team := auth.Team{Name: "angra"}
	err := s.conn.Teams().Insert(team)
	c.Assert(err, check.IsNil)
	token := userWithPermission(c, permission.Permission{
		Scheme:  permission.PermAppCreate,
		Context: permission.Context(permission.CtxTeam, "angra"),
	})
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool(pool.Name, []string{"angra"})
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	opts = provision.AddPoolOptions{Name: "nopool"}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("nopool")
	defaultPool, err := provision.GetDefaultPool()
	c.Assert(err, check.IsNil)
	expected := []provision.Pool{
		*defaultPool,
		{Name: "pool1"},
	}
	req, err := http.NewRequest("GET", "/pools", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	err = poolList(rec, req, token)
	c.Assert(err, check.IsNil)
	var pools []provision.Pool
	err = json.NewDecoder(rec.Body).Decode(&pools)
	c.Assert(err, check.IsNil)
	c.Assert(pools, check.DeepEquals, expected)
}

func (s *S) TestPoolListEmptyHandler(c *check.C) {
	_, err := s.conn.Pools().RemoveAll(nil)
	c.Assert(err, check.IsNil)
	u := auth.User{Email: "passing-by@angra.com", Password: "123456"}
	_, err = nativeScheme.Create(&u)
	c.Assert(err, check.IsNil)
	defer s.conn.Users().Remove(bson.M{"email": u.Email})
	token, err := nativeScheme.Login(map[string]string{"email": u.Email, "password": "123456"})
	c.Assert(err, check.IsNil)
	defer s.conn.Tokens().Remove(bson.M{"token": token.GetValue()})
	req, err := http.NewRequest("GET", "/pools", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", "b "+token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusNoContent)
}

func (s *S) TestPoolListHandlerWithPermissionToDefault(c *check.C) {
	team := auth.Team{Name: "angra"}
	err := s.conn.Teams().Insert(team)
	c.Assert(err, check.IsNil)
	perms := []permission.Permission{
		{
			Scheme:  permission.PermAppCreate,
			Context: permission.Context(permission.CtxGlobal, ""),
		},
		{
			Scheme:  permission.PermPoolUpdate,
			Context: permission.Context(permission.CtxGlobal, ""),
		},
	}
	token := userWithPermission(c, perms...)
	pool := provision.Pool{Name: "pool1"}
	opts := provision.AddPoolOptions{Name: pool.Name, Default: pool.Default}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool(pool.Name, []string{team.Name})
	c.Assert(err, check.IsNil)
	defer provision.RemovePool(pool.Name)
	req, err := http.NewRequest("GET", "/pools", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	err = poolList(rec, req, token)
	c.Assert(err, check.IsNil)
	var pools []provision.Pool
	err = json.NewDecoder(rec.Body).Decode(&pools)
	c.Assert(err, check.IsNil)
	c.Assert(pools, check.HasLen, 2)
	c.Assert(pools[0].Name, check.Equals, "test1")
	c.Assert(pools[1].Name, check.Equals, "pool1")
}

func (s *S) TestPoolUpdateToPublicHandler(c *check.C) {
	opts := provision.AddPoolOptions{Name: "pool1"}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	p, err := provision.GetPoolByName("pool1")
	c.Assert(err, check.IsNil)
	_, err = p.GetTeams()
	c.Assert(err, check.NotNil)
	defer provision.RemovePool("pool1")
	b := bytes.NewBufferString("public=true")
	req, err := http.NewRequest("PUT", "/pools/pool1", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	c.Assert(err, check.IsNil)
	teams, err := p.GetTeams()
	c.Assert(err, check.IsNil)
	c.Assert(teams, check.DeepEquals, []string{"tsuruteam"})
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.update",
		StartCustomData: []map[string]interface{}{
			{"name": ":name", "value": "pool1"},
			{"name": "public", "value": "true"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestPoolUpdateToDefaultPoolHandler(c *check.C) {
	provision.RemovePool("test1")
	opts := provision.AddPoolOptions{Name: "pool1"}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool1")
	b := bytes.NewBufferString("default=true")
	req, err := http.NewRequest("PUT", "/pools/pool1", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	c.Assert(err, check.IsNil)
	p, err := provision.GetPoolByName("pool1")
	c.Assert(err, check.IsNil)
	c.Assert(p.Default, check.Equals, true)
}

func (s *S) TestPoolUpdateOverwriteDefaultPoolHandler(c *check.C) {
	provision.RemovePool("test1")
	opts := provision.AddPoolOptions{Name: "pool1", Default: true}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool1")
	opts = provision.AddPoolOptions{Name: "pool2"}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool2")
	b := bytes.NewBufferString("default=true&force=true")
	req, err := http.NewRequest("PUT", "/pools/pool2", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	p, err := provision.GetPoolByName("pool2")
	c.Assert(err, check.IsNil)
	c.Assert(p.Default, check.Equals, true)
}

func (s *S) TestPoolUpdateNotOverwriteDefaultPoolHandler(c *check.C) {
	provision.RemovePool("test1")
	opts := provision.AddPoolOptions{Name: "pool1", Default: true}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool1")
	opts = provision.AddPoolOptions{Name: "pool2"}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool2")
	b := bytes.NewBufferString("default=true")
	request, err := http.NewRequest("PUT", "/pools/pool2", b)
	c.Assert(err, check.IsNil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "bearer "+s.token.GetValue())
	recorder := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(recorder, request)
	c.Assert(recorder.Code, check.Equals, http.StatusConflict)
	c.Assert(recorder.Body.String(), check.Equals, provision.ErrDefaultPoolAlreadyExists.Error()+"\n")
}

func (s *S) TestPoolUpdateProvisioner(c *check.C) {
	provision.RemovePool("test1")
	opts := provision.AddPoolOptions{Name: "pool1", Public: true, Default: true}
	err := provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	defer provision.RemovePool("pool1")
	b := bytes.NewBufferString("provisioner=myprov&default=false")
	req, err := http.NewRequest("PUT", "/pools/pool1", b)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	c.Assert(err, check.IsNil)
	p, err := provision.GetPoolByName("pool1")
	c.Assert(err, check.IsNil)
	c.Assert(p.Provisioner, check.Equals, "myprov")
	c.Assert(p.Default, check.Equals, false)
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "pool1"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.update",
		StartCustomData: []map[string]interface{}{
			{"name": ":name", "value": "pool1"},
			{"name": "default", "value": "false"},
			{"name": "provisioner", "value": "myprov"},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestPoolUpdateNotFound(c *check.C) {
	b := bytes.NewBufferString("public=true")
	request, err := http.NewRequest("PUT", "/pools/not-found", b)
	c.Assert(err, check.IsNil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "bearer "+s.token.GetValue())
	recorder := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(recorder, request)
	c.Assert(recorder.Code, check.Equals, http.StatusNotFound)
}

func (s *S) TestPoolConstraint(c *check.C) {
	err := provision.SetPoolConstraints("*", "router=*")
	c.Assert(err, check.IsNil)
	err = provision.SetPoolConstraints("dev", "router=dev")
	c.Assert(err, check.IsNil)
	expected := []provision.PoolConstraint{
		{PoolExpr: "test1", Field: "team", Values: []string{"*"}, WhiteList: true},
		{PoolExpr: "*", Field: "router", Values: []string{"*"}, WhiteList: true},
		{PoolExpr: "dev", Field: "router", Values: []string{"dev"}, WhiteList: true},
	}
	request, err := http.NewRequest("GET", "/constraints", nil)
	c.Assert(err, check.IsNil)
	request.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, request)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	var constraints []provision.PoolConstraint
	err = json.NewDecoder(rec.Body).Decode(&constraints)
	c.Assert(err, check.IsNil)
	c.Assert(constraints, check.DeepEquals, expected)
}

func (s *S) TestPoolConstraintListEmpty(c *check.C) {
	err := provision.SetPoolConstraints("test1", "team=")
	c.Assert(err, check.IsNil)
	request, err := http.NewRequest("GET", "/constraints", nil)
	c.Assert(err, check.IsNil)
	request.Header.Set("Authorization", "bearer "+s.token.GetValue())
	recorder := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(recorder, request)
	c.Assert(recorder.Code, check.Equals, http.StatusNoContent)
}

func (s *S) TestPoolConstraintSet(c *check.C) {
	values := url.Values{}
	values.Set("poolExpr", "*")
	values["constraints"] = []string{"team!=*", "router=routerA"}
	req, err := http.NewRequest("PUT", "/constraints", strings.NewReader(values.Encode()))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusOK)
	expected := []*provision.PoolConstraint{
		{PoolExpr: "test1", Field: "team", Values: []string{"*"}, WhiteList: true},
		{PoolExpr: "*", Field: "team", Values: []string{"*"}, WhiteList: false},
		{PoolExpr: "*", Field: "router", Values: []string{"routerA"}, WhiteList: true},
	}
	constraints, err := provision.ListPoolsConstraints(nil)
	c.Assert(err, check.IsNil)
	c.Assert(constraints, check.DeepEquals, expected)
	c.Assert(eventtest.EventDesc{
		Target: event.Target{Type: event.TargetTypePool, Value: "*"},
		Owner:  s.token.GetUserName(),
		Kind:   "pool.update.constraint.set",
		StartCustomData: []map[string]interface{}{
			{"name": "poolExpr", "value": "*"},
			{"name": "constraints", "value": []interface{}{"team!=*", "router=routerA"}},
		},
	}, eventtest.HasEvent)
}

func (s *S) TestPoolConstraintSetRequiresPoolExpr(c *check.C) {
	req, err := http.NewRequest("PUT", "/constraints", bytes.NewBufferString(""))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusBadRequest)
	c.Assert(rec.Body.String(), check.Equals, "You must provide a Pool Expression\n")
}

func (s *S) TestPoolConstraintSetRequiresAtLeastOneConstraint(c *check.C) {
	values := url.Values{}
	values.Set("poolExpr", "*")
	req, err := http.NewRequest("PUT", "/constraints", strings.NewReader(values.Encode()))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "bearer "+s.token.GetValue())
	rec := httptest.NewRecorder()
	m := RunServer(true)
	m.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, http.StatusBadRequest)
	c.Assert(rec.Body.String(), check.Equals, "You must provide at least one constraint to set\n")
}
