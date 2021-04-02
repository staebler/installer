package terraform

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/openshift/installer/data"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/openshift/installer/pkg/lineprinter"
	texec "github.com/openshift/installer/pkg/terraform/exec"
	"github.com/openshift/installer/pkg/terraform/exec/plugins"
)

const (
	// StateFileName is the default name for Terraform state files.
	StateFileName string = "terraform.tfstate"

	// VarFileName is the default name for Terraform var file.
	VarFileName string = "terraform.tfvars"
)

// Apply unpacks the platform-specific Terraform modules into the
// given directory and then runs 'terraform init' and 'terraform
// apply'.  It returns the absolute path of the tfstate file, rooted
// in the specified directory, along with any errors from Terraform.
func Apply(dir string, platform string, extraArgs ...string) (path string, err error) {
	pwd, err := changeDir(dir)
	if err != nil {
		return "", err
	}
	defer os.Chdir(pwd)

	err = unpackAndInit(platform)
	if err != nil {
		return "", err
	}

	defaultArgs := []string{
		"-auto-approve",
		"-input=false",
		fmt.Sprintf("-state=%s", filepath.Join(dir, StateFileName)),
		fmt.Sprintf("-state-out=%s", filepath.Join(dir, StateFileName)),
	}
	args := append(defaultArgs, extraArgs...)
	args = append(args, dir)
	sf := filepath.Join(dir, StateFileName)

	lpDebug := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Debug}).Print}
	lpError := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Error}).Print}
	defer lpDebug.Close()
	defer lpError.Close()

	errBuf := &bytes.Buffer{}
	if exitCode := texec.Apply(args, lpDebug, io.MultiWriter(errBuf, lpError)); exitCode != 0 {
		return sf, errors.Wrap(Diagnose(errBuf.String()), "failed to apply Terraform")
	}
	return sf, nil
}

// Destroy unpacks the platform-specific Terraform modules into the
// given directory and then runs 'terraform init' and 'terraform
// destroy'.
func Destroy(dir string, platform string, extraArgs ...string) (err error) {
	pwd, err := changeDir(dir)
	if err != nil {
		return err
	}
	defer os.Chdir(pwd)

	err = unpackAndInit(platform)
	if err != nil {
		return err
	}

	defaultArgs := []string{
		"-auto-approve",
		"-input=false",
		fmt.Sprintf("-state=%s", filepath.Join(dir, StateFileName)),
		fmt.Sprintf("-state-out=%s", filepath.Join(dir, StateFileName)),
	}
	args := append(defaultArgs, extraArgs...)
	args = append(args, dir)

	lpDebug := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Debug}).Print}
	lpError := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Error}).Print}
	defer lpDebug.Close()
	defer lpError.Close()

	if exitCode := texec.Destroy(args, lpDebug, lpError); exitCode != 0 {
		return errors.New("failed to destroy using Terraform")
	}
	return nil
}

// unpack unpacks the platform-specific Terraform modules into the
// given directory.
func unpack(platform string) (err error) {
	err = data.Unpack(".", platform)
	if err != nil {
		return err
	}

	err = data.Unpack("config.tf", "config.tf")
	if err != nil {
		return err
	}

	err = data.Unpack("terraform.rc", "terraform.rc")
	if err != nil {
		return err
	}

	return nil
}

// unpackAndInit unpacks the platform-specific Terraform modules into
// the given directory and then runs 'terraform init'.
func unpackAndInit(platform string) (err error) {
	err = unpack(platform)
	if err != nil {
		return errors.Wrap(err, "failed to unpack Terraform modules")
	}

	if err := setupEmbeddedPlugins(); err != nil {
		return errors.Wrap(err, "failed to setup embedded Terraform plugins")
	}

	lpDebug := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Debug}).Print}
	lpError := &lineprinter.LinePrinter{Print: (&lineprinter.Trimmer{WrappedPrint: logrus.Error}).Print}
	defer lpDebug.Close()
	defer lpError.Close()

	os.Setenv("TF_CLI_CONFIG_FILE", "terraform.rc")

	// XXX: These are only here for debugging CI
	os.Setenv("TF_LOG", "trace")

	args := []string{
		fmt.Sprintf("-plugin-dir=%s", filepath.Join("plugins")),
	}
	args = append(args, ".")
	if exitCode := texec.Init(args, lpDebug, lpError); exitCode != 0 {
		return errors.New("failed to initialize Terraform")
	}
	return nil
}

func setupEmbeddedPlugins() error {
	execPath, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "failed to find path for the executable")
	}

	pdir := filepath.Join("plugins", "openshift", "local")
	for name, plugin := range plugins.KnownPlugins {
		dstDir := filepath.Join(pdir, plugin.Name, plugin.Version, fmt.Sprintf("linux_%s", runtime.GOARCH))
		if err := os.MkdirAll(dstDir, 0777); err != nil {
			return err
		}

		dst := filepath.Join(dstDir, name)
		if runtime.GOOS == "windows" {
			dst = fmt.Sprintf("%s.exe", dst)
		}
		if _, err := os.Stat(dst); err == nil {
			// stat succeeded, the plugin already exists.
			continue
		}
		logrus.Debugf("Symlinking plugin %s src: %q dst: %q", name, execPath, dst)
		if err := os.Symlink(execPath, dst); err != nil {
			return err
		}
	}
	return nil
}

func changeDir(dir string) (string, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "could not get working directory")
	}
	if err := os.Chdir(dir); err != nil {
		return "", errors.Wrap(err, "could not change to data directory")
	}
	return pwd, nil
}