package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aptly-dev/aptly/deb"
	"github.com/aptly-dev/aptly/utils"
	"github.com/smira/commander"
)

func aptlyGraph(cmd *commander.Command, args []string) error {
	var err error

	if len(args) != 0 {
		cmd.Usage()
		return commander.ErrCommandError
	}

	layout := context.Flags().Lookup("layout").Value.String()

	fmt.Printf("Generating graph...\n")
	collectionFactory := context.NewCollectionFactory()
	graph, err := deb.BuildGraph(collectionFactory, layout)
	if err != nil {
		return err
	}

	buf := bytes.NewBufferString(graph.String())

	tempfile, err := os.CreateTemp("", "aptly-graph")
	if err != nil {
		return err
	}
	_ = tempfile.Close()
	_ = os.Remove(tempfile.Name())

	format := context.Flags().Lookup("format").Value.String()
	output := context.Flags().Lookup("output").Value.String()

	if filepath.Ext(output) != "" {
		format = filepath.Ext(output)[1:]
	}

	tempfilename := tempfile.Name() + "." + format

	command := exec.Command("dot", "-T"+format, "-o"+tempfilename)
	command.Stderr = os.Stderr

	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}

	err = command.Start()
	if err != nil {
		return fmt.Errorf("unable to execute dot: %s (is graphviz package installed?)", err)
	}

	_, err = io.Copy(stdin, buf)
	if err != nil {
		return err
	}

	err = stdin.Close()
	if err != nil {
		return err
	}

	err = command.Wait()
	if err != nil {
		return err
	}

	if output != "" {
		err = utils.CopyFile(tempfilename, output)
		if err != nil {
			return fmt.Errorf("unable to copy %s -> %s: %s", tempfilename, output, err)
		}

		fmt.Printf("Output saved to %s\n", output)
		_ = os.Remove(tempfilename)
	} else {
		command := getOpenCommand()
		fmt.Printf("Displaying %s file: %s %s\n", format, command, tempfilename)

		args := strings.Split(command, " ")

		viewer := exec.Command(args[0], append(args[1:], tempfilename)...)
		viewer.Stderr = os.Stderr
		err = viewer.Start()
	}

	return err
}

// getOpenCommand tries to guess command to open image for OS
func getOpenCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return "/usr/bin/open"
	case "windows":
		return "cmd /c start"
	default:
		return "xdg-open"
	}
}

func makeCmdGraph() *commander.Command {
	cmd := &commander.Command{
		Run:       aptlyGraph,
		UsageLine: "graph",
		Short:     "render graph of relationships",
		Long: `
Command graph displays relationship between mirrors, local repositories,
snapshots and published repositories using graphviz package to render
graph as an image.

Example:

  $ aptly graph
`,
	}

	cmd.Flag.String("format", "png", "render graph to specified format (png, svg, pdf, etc.)")
	cmd.Flag.String("output", "", "specify output filename, default is to open result in viewer")
	cmd.Flag.String("layout", "horizontal", "create a more 'vertical' or a more 'horizontal' graph layout")

	return cmd
}
