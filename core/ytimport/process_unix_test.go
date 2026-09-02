//go:build unix && !aix && !solaris

package ytimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("yt-dlp cancellation", func() {
	It("kills child processes", func() {
		dir := GinkgoT().TempDir()
		pidPath := filepath.Join(dir, "child.pid")
		script := fmt.Sprintf("#!/bin/sh\n/bin/sleep 30 &\necho $! > %q\nwait\n", pidPath)
		Expect(os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", dir)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, _, err := runYtdlp(ctx)
			done <- err
		}()
		Eventually(pidPath).Should(BeAnExistingFile())
		contents, err := os.ReadFile(pidPath)
		Expect(err).ToNot(HaveOccurred())
		pid, err := strconv.Atoi(strings.TrimSpace(string(contents)))
		Expect(err).ToNot(HaveOccurred())

		cancel()
		Eventually(done, time.Second).Should(Receive(HaveOccurred()))
		Eventually(func() bool {
			return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
		}, time.Second).Should(BeTrue())
	})
})
