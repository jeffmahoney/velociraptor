package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/magefile/mage/target"
)

var bpfModuleList = []string{
	"vql/linux/tcpsnoop/tcpsnoop.bpf.o",
	"vql/linux/dnssnoop/dnssnoop.bpf.o",
}

type BPFBuildEnv struct {
	baseDir   string
	cflags    []string
	clangarch string
	bpfarch   string
	bpftool   string
	built     bool
}

func (self *BPFBuildEnv) outputDir() string {
	return filepath.Join(self.baseDir, "output")
}

func (self *BPFBuildEnv) updateSubmodule() error {
	fmt.Println("INFO: updating submodule 'libbpfgo'")

	return sh.Run("git", "submodule", "update", "--init", "--recursive",
		self.baseDir)
}

func (self *BPFBuildEnv) createoutputDir() error {
	err := os.Mkdir(self.outputDir(), 0o755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func (self *BPFBuildEnv) vmlinuxDotHPath() string {
	return filepath.Join(self.outputDir(), "vmlinux.h")
}

func (self *BPFBuildEnv) vmlinuxDotHNeeded() (bool, error) {
	path := self.vmlinuxDotHPath()

	_, err := os.Stat(path)
	if err == nil {
		return false, nil
	} else if os.IsNotExist(err) {
		return true, nil
	}

	return true, err
}

func (self *BPFBuildEnv) buildVmlinuxDotH() error {
	mg.Deps(self.createoutputDir)

	needed, err := self.vmlinuxDotHNeeded()
	if err != nil || !needed {
		return err
	}

	args := []string{"btf", "dump", "file", "/sys/kernel/btf/vmlinux", "format", "c"}

	output, err := sh.Output(self.bpftool, args...)
	if err != nil {
		return err
	}

	f, err := os.Create(self.vmlinuxDotHPath())
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(output)
	return nil
}

func (self *BPFBuildEnv) buildLibbpf() error {
	mg.Deps(self.updateSubmodule)
	mg.Deps(self.buildVmlinuxDotH)

	return sh.RunWith(self.Env(), "make", "-C", self.baseDir, "libbpfgo-static")
}

func (self *BPFBuildEnv) buildModule(targetPath string) error {
	mg.Deps(self.buildLibbpf)

	srcPath := targetPath[:len(targetPath)-2] + ".c"

	needed, err := target.Path(targetPath, srcPath)
	if err != nil {
		return err
	}

	if !needed {
		return nil
	}

	buildflags := []string{
		"-target", "bpf",
		fmt.Sprintf("-D__TARGET_ARCH_%s", self.bpfarch),
		fmt.Sprintf("-D__%s__", self.clangarch),
		fmt.Sprintf("-I%s", self.outputDir()),
		fmt.Sprintf("-I%s", filepath.Join(self.baseDir, "include", "uapi")),
		"-c", srcPath,
		"-o", targetPath,
	}

	args := append(self.cflags, buildflags...)

	err = sh.Run("clang", args...)
	if err != nil {
		return err
	}

	return sh.Run("llvm-strip", "-g", targetPath)
}

func (self *BPFBuildEnv) Build() (bool, error) {
	build := os.Getenv("BUILD_BPF_PLUGINS")
	disabled := false
	required := false
	if build != "" {
		disabled = build == "0"
		required = build == "1"
	}

	// Silently skip
	if runtime.GOOS != "linux" || disabled || len(bpfModuleList) == 0 {
		return false, nil
	}

	self.clangarch = runtime.GOARCH
	self.bpfarch = runtime.GOARCH

	switch runtime.GOARCH {
	case "amd64":
		self.clangarch = "x86_64"
		self.bpfarch = "x86"
	case "arm64":
		self.clangarch = "aarch64"
	case "ppc64le":
		self.clangarch = "ppc64"
		self.bpfarch = "powerpc"
	case "s390x":
		self.bpfarch = "s390"
	default:
		// Succeed with warning
		fmt.Printf("INFO: BPF support is not implemented on %s\n", runtime.GOARCH)
		return false, nil
	}

	// Check external dependencies
	missing := []string{}

	_, err := exec.LookPath("clang")
	if err != nil {
		missing = append(missing, "clang")
	}

	_, err = exec.LookPath("llvm-strip")
	if err != nil {
		missing = append(missing, "llvm-strip")
	}

	// We only need bpftool if the user hasn't provided vmlinux.h
	needed, err := self.vmlinuxDotHNeeded()
	if err != nil {
		return false, err
	}
	if needed {
		self.bpftool, err = exec.LookPath("bpftool")
		if err != nil {
			// Some systems install bpftool in /usr/sbin and it's probably not
			// in the unprivileged user's path
			self.bpftool, err = exec.LookPath("/usr/sbin/bpftool")
		}
		if err != nil {
			missing = append(missing, "bpftool")
		}
	}

	if len(missing) > 0 {
		for _, tool := range missing {
			fmt.Printf("INFO: Cannot build BPF objects without %s installed.\n", tool)
		}
		if required {
			fmt.Println("ERROR: Either install the missing tools or build without BUILD_BPF_PLUGINS=1")
			return false, fmt.Errorf("Missing external tools: %s", strings.Join(missing, ", "))
		} else {
			fmt.Println("INFO: To build BPF plugins, install the required tools")
			return false, nil
		}
	}

	for _, module := range bpfModuleList {
		mg.Deps(mg.F(self.buildModule, module))
	}

	self.built = true
	return true, nil
}

func (self *BPFBuildEnv) Env() map[string]string {
	env := make(map[string]string)
	if self.built {
		path, err := filepath.Abs(self.outputDir())
		if err != nil {
			path = "."
		}

		env["CGO_CFLAGS"] = fmt.Sprintf("-I%s", path)
		env["CGO_LDFLAGS"] = fmt.Sprintf("%s/libbpf.a -l:libelf.a -lz -lzstd", path)
	}

	return env
}

func (self *BPFBuildEnv) Tags() string {
	tags := ""
	if self.built {
		tags = "linuxbpf libbpfgo_static"
	}
	return tags
}

func (self *BPFBuildEnv) Clean() {
	_, err := os.Stat(self.baseDir)
	if err == nil {
		sh.Run("make", "-C", self.baseDir, "clean")
	}

	for _, module := range bpfModuleList {
		os.Remove(module)
	}
}

func NewBPFBuildEnv() *BPFBuildEnv {
	return &BPFBuildEnv{
		baseDir: filepath.Join("third_party", "libbpfgo"),
		cflags:  []string{"-g", "-O2", "-Wall"},
	}
}
