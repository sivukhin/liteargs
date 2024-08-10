package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"text/template"
	"time"

	"github.com/fatih/color"
	_ "github.com/fatih/color"
	"github.com/libsql/libsql-shell-go/pkg/shell"
	_ "github.com/libsql/libsql-shell-go/pkg/shell"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	errorHeader = color.New(color.FgRed, color.Bold)
	infoHeader  = color.New(color.FgHiWhite, color.Bold)
	traceHeader = color.New(color.FgWhite, color.Italic)
	okHeader    = color.New(color.FgGreen, color.Italic)
)

func fatalLog(format string, args ...any) {
	errorLog(format, args...)
	os.Exit(1)
}

func errorLog(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "%v%v\n", errorHeader.Sprintf("error: "), fmt.Sprintf(format, args...))
}

func infoLog(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "%v%v\n", infoHeader.Sprintf("info : "), fmt.Sprintf(format, args...))
}

func okLog(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "%v%v\n", okHeader.Sprintf("ok   : "), fmt.Sprintf(format, args...))
}

func traceLog(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "%v%v\n", traceHeader.Sprintf("trace: "), fmt.Sprintf(format, args...))
}

func separator(s string) rune {
	if s == "\\t" {
		return '\t'
	}
	runes := []rune(s)
	if len(runes) != 1 {
		fatalLog("separate must be a single character, got: '%v'", s)
	}
	return runes[0]
}

func repeat(s string, n int) []string {
	r := make([]string, n)
	for i := range r {
		r[i] = s
	}
	return r
}

func anyArray[T any](a []T) []any {
	r := make([]any, len(a))
	for i := range r {
		r[i] = a[i]
	}
	return r
}

func input(file string) io.ReadCloser {
	if file != "" {
		reader, err := os.Open(file)
		if err != nil {
			fatalLog("failed to open input file %v: %v", file, err)
		}
		return reader
	}
	return io.NopCloser(os.Stdin)
}

func render(command string, rows []map[string]any) ([]string, error) {
	t, err := template.New("liteargs").Parse(command)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	buffer := make([]byte, 0, 1024)
	writer := bytes.NewBuffer(buffer)
	commands := make([]string, 0, len(rows))
	for _, row := range rows {
		err = t.Execute(writer, row)
		if err != nil {
			return nil, fmt.Errorf("failed to render template: %w", err)
		}
		commands = append(commands, writer.String())
		writer.Reset()
	}
	return commands, nil
}

func run(ctx context.Context, shell string, command string) (bool, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(shell, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Start()
	if err != nil {
		errorLog("failed to execute command: %v, err=%v", command, err)
		return false, "", ""
	}
	traceLog("command started: %v", command)

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case err = <-waitCh:
		if err == nil {
			okLog("command succeed: %v, elapsed=%v", command, time.Since(startTime))
			return true, stdout.String(), stderr.String()
		}
		errorLog("command failed: %v, err=%v", command, err)
	case <-ctx.Done():
		traceLog("command interrupted: %v", command)
		err = cmd.Process.Signal(syscall.SIGINT)
		if err != nil {
			traceLog("command interruption failed: %v, err=%v", command, err)
		}
		_ = cmd.Process.Kill()
	}
	return false, stdout.String(), stderr.String()
}

func main() {
	var (
		execParallelism int
		execTake        int
		execFilter      string
		execOrder       string
		execShell       string
		execShow        bool
	)
	var execCmd = &cobra.Command{
		Use:   "exec [state.db] [command]",
		Short: "Execute a command with the state database",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			db, err := NewLiteArgsDb(args[0])
			if err != nil {
				fatalLog("%v", err)
			}
			rows, pks, err := db.Filter(LiteArgsDbFilter{
				Take:   execTake,
				Filter: execFilter,
				Order:  execOrder,
			})
			if err != nil {
				fatalLog("%v", err)
			}
			commands, err := render(args[1], rows)
			if err != nil {
				fatalLog("%v", err)
			}
			if execShow {
				for _, command := range commands {
					fmt.Println(command)
				}
				return
			}

			var group errgroup.Group
			group.SetLimit(execParallelism)

			startTime := time.Now()
			succeedCnt, failedCnt := int32(0), int32(0)
			for i, command := range commands {
				group.Go(func() error {
					succeed, stdout, stderr := run(cmd.Context(), execShell, command)
					err := db.Update(pks[i], succeed, stdout, stderr, time.Now())
					if err != nil {
						traceLog("%v", err)
					}
					if succeed && err == nil {
						atomic.AddInt32(&succeedCnt, 1)
					} else {
						atomic.AddInt32(&failedCnt, 1)
					}
					return nil
				})
			}
			_ = group.Wait()
			infoLog("succeed: %v, failed: %v, elapsed=%v", succeedCnt, failedCnt, time.Since(startTime))
		},
	}
	execCmd.Flags().IntVarP(&execParallelism, "parallelism", "p", 1, "maximum execution parallelism")
	execCmd.Flags().IntVarP(&execTake, "take", "t", 0, "execute command only for first element; -1 removes any limits")
	execCmd.Flags().StringVar(&execFilter, "filter", "", "arbitrary SQL filter")
	execCmd.Flags().StringVar(&execOrder, "order", "", "arbitrary SQL filter")
	execCmd.Flags().StringVar(&execShell, "shell", "sh", "shell for commands execution")
	execCmd.Flags().BoolVar(&execShow, "show", false, "show commands but do not execute them")

	var inspectCmd = &cobra.Command{
		Use:   "shell [state.db]",
		Short: "Shell into the liteargs state database",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			emptyWelcome := ""
			err := shell.RunShell(shell.ShellConfig{
				DbUri:          args[0],
				InF:            os.Stdin,
				OutF:           os.Stdout,
				ErrF:           os.Stderr,
				WelcomeMessage: &emptyWelcome,
			})
			if err != nil {
				fatalLog("failed to exit shell: %v", err)
			}
		},
	}

	var resetCmd = &cobra.Command{
		Use:   "reset [state.db]",
		Short: "Reset the state database",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			db, err := NewLiteArgsDb(args[0])
			if err != nil {
				fatalLog("%v", err)
			}
			if err = db.Reset(); err != nil {
				fatalLog("%v", err)
			}
		},
	}

	var (
		loadNoHeader bool
		loadSep      string
		loadInput    string
	)
	var loadCmd = &cobra.Command{
		Use:   "load [state.db]",
		Short: "Load the state database",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			db, err := NewLiteArgsDb(args[0])
			if err != nil {
				fatalLog("%v", err)
			}

			reader := input(loadInput)
			defer reader.Close()

			csvReader := csv.NewReader(reader)
			csvReader.Comma = separator(loadSep)

			var header []string
			lineNumber, recordNumber := 0, 0
			for {
				lineNumber++
				records, err := csvReader.Read()
				if errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					fatalLog("failed to read csv line %v: err=%v", lineNumber, err)
				}

				if lineNumber == 1 && loadNoHeader {
					header = make([]string, len(records))
					for i := range header {
						header[i] = fmt.Sprintf("arg%d", i)
					}
				} else if lineNumber == 1 {
					header = records
				}

				if lineNumber == 1 {
					err = db.Init(header)
					if err != nil {
						fatalLog("%v", err)
					}
					if !loadNoHeader {
						continue
					}
				}

				recordNumber++
				err = db.Insert(records)
				if err != nil {
					fatalLog("%v, line=%v", err, lineNumber)
				}
			}
			infoLog("successfully loaded %v records", recordNumber)
		},
	}
	loadCmd.Flags().StringVarP(&loadInput, "input", "i", "", "input file with data")
	loadCmd.Flags().StringVarP(&loadSep, "separator", "s", ",", "CSV separator")
	loadCmd.Flags().BoolVar(&loadNoHeader, "no-header", false, "CSV have no header provided")

	var rootCmd = &cobra.Command{Use: "liteargs"}
	rootCmd.AddCommand(execCmd, inspectCmd, resetCmd, loadCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
