//go:build !unix || aix || solaris

package ytimport

import "os/exec"

func configureCommand(*exec.Cmd) {}
