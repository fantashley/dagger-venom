// A module for running integration tests using the Venom framework.
//
// Venom is a framework for managing integration test suites. The tests
// are defined in YAML files that are mounted and executed by the Venom
// container. Results are outputted into a directory that is returned
// by the function that runs tests.

package main

import (
	"context"
	"dagger/dagger-venom/internal/dagger"
	"fmt"
	"golang.org/x/mod/modfile"
	"path/filepath"
	"runtime"
	"strconv"
)

type DaggerVenom struct{}

// Venom returns a lightweight container with the Venom CLI binary.
func (m *DaggerVenom) Venom(ctx context.Context) (*dagger.Container, error) {
	venomSrc := dag.Git("https://github.com/ovh/venom").Head().Tree()

	modContents, err := venomSrc.File("go.mod").Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("error reading go.mod: %w", err)
	}

	modFile, err := modfile.ParseLax("go.mod", []byte(modContents), nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing go.mod: %w", err)
	}

	workDir := "/venom"
	build := dag.
		Container().
		From("golang:"+modFile.Go.Version).
		WithDirectory(workDir, venomSrc).
		WithWorkdir(workDir).
		WithExec([]string{"make", "build", "OS=" + runtime.GOOS, "ARCH=" + runtime.GOARCH}).
		Directory(filepath.Join(workDir, "dist"))

	return dag.Container().From("alpine:latest").
		WithFile("/usr/local/venom", build.File(fmt.Sprintf("venom.%s-%s", runtime.GOOS, runtime.GOARCH))), nil
}

// TestResults contains the directory that Venom outputs its results to,
// as well as the exit code that was returned by "venom test".
type TestResults struct {
	ResultsDir *dagger.Directory
	ExitCode   int
}

// Test runs Venom against the test suite provided via the tests argument
// and returns the results of the tests.
func (m *DaggerVenom) Test(ctx context.Context, tests *dagger.Directory) (TestResults, error) {
	venom, err := m.Venom(ctx)
	if err != nil {
		return TestResults{}, fmt.Errorf("error building venom container: %w", err)
	}

	workDir := "/workdir"
	testsDir := "tests"
	exitCodeFile := "exit_code"
	testsPath := filepath.Join(workDir, testsDir)
	resultsPath := filepath.Join(workDir, "results")

	testContainer, err := venom.
		WithWorkdir(workDir).
		WithMountedDirectory(testsPath, tests).
		WithEnvVariable("VENOM_OUTPUT_DIR", resultsPath).
		WithEnvVariable("VENOM_LIB_DIR", filepath.Join(testsPath, "lib")).
		WithEnvVariable("VENOM_VERBOSE", "1").
		WithExec([]string{"/bin/sh", "-c", "/usr/local/venom run ./" + testsDir + "/*.y*ml --html-report; echo -n $? > " + exitCodeFile}).
		Sync(ctx)
	if err != nil {
		return TestResults{}, fmt.Errorf("unexpected error executing tests: %w", err)
	}

	exitCodeStr, err := testContainer.File(filepath.Join(workDir, exitCodeFile)).Contents(ctx)
	if err != nil {
		return TestResults{}, fmt.Errorf("could not get error code from test command: %w", err)
	}

	exitCode, err := strconv.Atoi(exitCodeStr)
	if err != nil {
		return TestResults{}, fmt.Errorf("invalid exit code for tests: %w", err)
	}

	return TestResults{ResultsDir: testContainer.Directory(resultsPath), ExitCode: exitCode}, nil
}
