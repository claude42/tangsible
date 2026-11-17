package ee

import (
	"strings"
	"testing"
)

func TestCheckContainerOptions(t *testing.T) {
	for _, c := range []struct {
		opts []string
		bad  string // substring of the error, "" when accepted
	}{
		{nil, ""},
		{[]string{"--cap-add=NET_RAW", "--device", "/dev/kvm", "--memory=2g", "--env-file=/x/env", "--init"}, ""},
		{[]string{"--cap-add", "NET_RAW"}, ""},
		{[]string{"--security-opt", "seccomp=unconfined"}, ""},
		{[]string{"--tmpfs", "/scratch:rw"}, ""},
		{[]string{"-p127.0.0.1:80:80/tcp"}, ""},
		{[]string{"-p", "8080:80", "--group-add=keep-groups"}, ""},
		{[]string{"--userns=host"}, "--userns"},
		{[]string{"--user", "root"}, "--user"},
		{[]string{"-u", "0"}, "-u"},
		{[]string{"-v", "/a:/b"}, "extra_mounts"},
		{[]string{"-v/a:/b"}, "extra_mounts"},
		{[]string{"--volume=/a:/b"}, "extra_mounts"},
		{[]string{"--mount", "type=bind,src=/a,dst=/b"}, "extra_mounts"},
		{[]string{"-w", "/x"}, "working directory"},
		{[]string{"--name=x"}, "--name"},
		{[]string{"--network=host"}, "network key"},
		{[]string{"-e", "A=b"}, "pass_env"},
		{[]string{"--env=A=b"}, "pass_env"},
		{[]string{"-it"}, "stdin"},
		{[]string{"-t"}, "terminal"},
		{[]string{"--entrypoint=/bin/sh"}, "--entrypoint"},
		{[]string{"--security-opt", "label=disable"}, "selinux key"},
		{[]string{"--security-opt=label=type:foo_t"}, "selinux key"},
		{[]string{"--tmpfs", ContainerHome + ":rw"}, "home directory"},
		{[]string{"--passwd-entry=x"}, "--passwd-entry"},
	} {
		err := CheckContainerOptions(c.opts)
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("%q: unexpected error %v", c.opts, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("%q: error %v, want one mentioning %q", c.opts, err, c.bad)
		}
	}
}

func TestContainerOptionsReachTheCommand(t *testing.T) {
	in := base()
	in.ContainerOptions = []string{"--cap-add=NET_RAW", "--memory", "2g"}
	argv := build(t, in).Argv
	img := -1
	for i, a := range argv {
		if a == "img:1" {
			img = i
		}
	}
	if img < 3 || argv[img-3] != "--cap-add=NET_RAW" || argv[img-2] != "--memory" || argv[img-1] != "2g" {
		t.Errorf("options should sit right before the image: %q", argv)
	}
	in.ContainerOptions = []string{"--userns=host"}
	if _, err := Build(in); err == nil {
		t.Error("Build should refuse a flag the builder owns")
	}
}
