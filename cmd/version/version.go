package version

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build information. Populated at build-time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	BuiltBy = "unknown"
)

// PrintInfo prints version information to stdout.
func PrintInfo() {
	fmt.Printf("Version:\t%s\n", Version)
	fmt.Printf("Git Commit:\t%s\n", Commit)
	fmt.Printf("Build Date:\t%s\n", Date)
	fmt.Printf("Built By:\t%s\n", BuiltBy)
	fmt.Printf("Go Version:\t%s\n", runtime.Version())
	fmt.Printf("Platform:\t%s/%s\n", runtime.GOOS, runtime.GOARCH)
}

// NewVersionCmd returns a cobra command that displays version information.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			PrintInfo()
		},
	}
}
