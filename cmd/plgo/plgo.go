package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/alecthomas/kong"
)

// CLI defines the command-line interface for plgo.
type CLI struct {
	Path      string           `arg:"" optional:"" default:"." help:"Path to the Go package to build." type:"existingdir"`
	Verbose   bool             `short:"v" help:"Verbose output (go build -x)."`
	Installer bool             `help:"Also produce a self-contained .sh installer script."`
	Output    string           `short:"o" help:"Output filename for the installer script." placeholder:"FILE"`
	Version   kong.VersionFlag `short:"V" help:"Print version and exit."`
}

// Run executes the build.
func (cmd *CLI) Run() error {
	moduleWriter, err := NewModuleWriter(cmd.Path)
	if err != nil {
		return fmt.Errorf("parse package: %w", err)
	}

	tempPackagePath, err := moduleWriter.WriteModule()
	if err != nil {
		return err
	}
	log.Println(tempPackagePath)

	if _, err = os.Stat("build"); os.IsNotExist(err) {
		if err = os.Mkdir("build", 0o744); err != nil {
			return fmt.Errorf("create build dir: %w", err)
		}
	}

	if err = buildPackage(tempPackagePath, moduleWriter.PackageName, cmd.Verbose); err != nil {
		return err
	}
	if err = moduleWriter.WriteSQL("build"); err != nil {
		return err
	}
	if err = moduleWriter.WriteControl("build"); err != nil {
		return err
	}
	if err = moduleWriter.WriteMakefile("build"); err != nil {
		return err
	}

	if cmd.Installer {
		outPath := cmd.Output
		if outPath == "" {
			outPath = "install-" + moduleWriter.PackageName + ".sh"
		}
		if err = moduleWriter.WriteInstaller("build", outPath); err != nil {
			return err
		}
	}

	return nil
}

func buildPackage(buildPath, packageName string, verbose bool) error {
	if err := os.Setenv("CGO_LDFLAGS_ALLOW", "-shared|-undefined|dynamic_lookup"); err != nil {
		return err
	}
	buildFlag := "-v"
	if verbose {
		buildFlag = "-x"
	}
	fileExt := ".so"
	if runtime.GOOS == "windows" {
		fileExt = ".dll"
	}
	absOut, err := filepath.Abs(filepath.Join("build", packageName+fileExt))
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	goBuild := exec.Command("go", "build", buildFlag,
		"-buildmode=c-shared",
		"-o", absOut,
	)
	goBuild.Dir = buildPath
	goBuild.Stdout = os.Stdout
	goBuild.Stderr = os.Stderr
	if err := goBuild.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	return nil
}

// version is set at build time via -ldflags.
var version = "0.2.0"

func main() {
	var cmd CLI
	ctx := kong.Parse(&cmd,
		kong.Name("plgo"),
		kong.Description("Build PostgreSQL extensions from Go packages.\n\nTurns exported Go functions into native PostgreSQL stored procedures,\ntriggers, and set-returning functions — no C required."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if err := ctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
