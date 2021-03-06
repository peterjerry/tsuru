.. Copyright 2014 tsuru authors. All rights reserved.
   Use of this source code is governed by a BSD-style
   license that can be found in the LICENSE file.

Gandalf
=======

To enable application deployment using git, tsuru uses Gandalf to manage Git repositories
used to push applications to. It's also responsible for setting hooks in these repositories
which will notify the tsuru API when a new deploy is made. For more details
check `Gandalf Documentation <http://gandalf.readthedocs.org/>`_

.. note::

    Gandalf is only required if you want to be able to deploy using ``git push``,
    without it you can still deploy applications using ``tsuru app-deploy``.

Gandalf will store and manage all Git repositories and SSH keys, as well as
users. When user runs a ``git push``, the communication happens directly between the
user host and the Gandalf host, and Gandalf will notify tsuru the new
deployment using a git hook.

++++++++++
Installing
++++++++++

Let's start adding the repositories for tsuru which contain the Gandalf package.

.. highlight:: bash

::

    sudo apt-get update
    sudo apt-get install curl python-software-properties
    sudo apt-add-repository ppa:tsuru/ppa -y
    sudo apt-get update

.. highlight:: bash

::

    sudo apt-get install gandalf-server

A deploy is executed in the ``git push``. In order to get it working, you will
need to add a pre-receive hook. tsuru comes with three pre-receive hooks, all
of them need further configuration:

    * s3cmd: uses `Amazon S3 <https://s3.amazonaws.com>`_ to store and serve
      archives
    * archive-server: uses tsuru's `archive-server
      <https://github.com/tsuru/archive-server>`_ to store and serve archives
    * swift: uses `Swift <http://swift.openstack.org>`_ to store and serve
      archives (compatible with `Rackspace Cloud Files
      <http://www.rackspace.com/cloud/files/>`_)

In this documentation, we will use archive-server, but you can use anything that
can store a git archive and serve it via HTTP or FTP. You can install archive-server via apt-get too:

.. highlight:: bash

::

    sudo apt-get install archive-server

Then you will need to configure Gandalf, install the pre-receive hook, set the
proper environment variables and start Gandalf and the archive-server, please note
that you should replace the value ``<your-machine-addr>`` with your machine public
address:

.. highlight:: bash

::

    sudo mkdir -p /home/git/bare-template/hooks
    sudo curl https://raw.githubusercontent.com/tsuru/tsuru/master/misc/git-hooks/pre-receive.archive-server -o /home/git/bare-template/hooks/pre-receive
    sudo chmod +x /home/git/bare-template/hooks/pre-receive
    sudo chown -R git:git /home/git/bare-template
    cat | sudo tee -a /home/git/.bash_profile <<EOF
    export ARCHIVE_SERVER_READ=http://<your-machine-addr>:3232 ARCHIVE_SERVER_WRITE=http://127.0.0.1:3131
    EOF

In the ``/etc/gandalf.conf`` file, remove the comment from the line "template:
/home/git/bare-template" and from the line "database", so it looks like that:

.. highlight:: yaml

::

    database:
      url: <your-mongodb-server>:27017
      name: gandalf

    git:
      bare:
        location: /var/lib/gandalf/repositories
        template: /home/git/bare-template

Then start gandalf and archive-server:

.. highlight:: bash

::

    sudo start gandalf-server
    sudo start archive-server

++++++++++++++++++++++++++++++++
Configuring tsuru to use Gandalf
++++++++++++++++++++++++++++++++

In order to use Gandalf, you need to change tsuru.conf accordingly:

#. Define "repo-manager" to use "gandalf";
#. Define "git:api-server" to point to the API of the Gandalf server
   (example: "http://localhost:8000");

For more details, please refer to the :doc:`configuration page
</reference/config>`.

+++++++++++++++++++++++++++++++++++++++
Token for authentication with tsuru API
+++++++++++++++++++++++++++++++++++++++

There is one last step in configuring Gandalf. It involves generating an access
token so that the hook we created can access the tsuru API. To do so, we need to export t
wo extra environment variables to the git user, which will run our deploy hooks, the URL
to our API server and a generated token.

First step is to generate a token in the machine where the API server is installed:

.. highlight:: bash

::

    $ tsurud token
    fed1000d6c05019f6550b20dbc3c572996e2c044


Now you have to go back to the machine you installed Gandalf, and run this:

.. highlight:: bash

::

    $ cat | sudo tee -a /home/git/.bash_profile <<EOF
    export TSURU_HOST=http://<your-tsuru-api-addr>:8080
    export TSURU_TOKEN=fed1000d6c05019f6550b20dbc3c572996e2c044
    EOF

+++++++++++++++++++++++++++++++++++++++++++++++++++
Adding Gandalf to an already existing tsuru cluster
+++++++++++++++++++++++++++++++++++++++++++++++++++

In the case of an old tsuru cluster running without Gandalf, users and
applications registered in tsuru won't be available in the newly created
Gandalf server, or both servers may be out-of-sync.

When Gandalf is enabled, administrators of the cloud can run the ``tsurud
gandalf-sync`` command.

++++++++++++++++++++++++
Managing SSH public keys
++++++++++++++++++++++++

In order to be able to send git pushes to the Git server :doc:`users need to have
their key registered in Gandalf</managing/repositories>`.
