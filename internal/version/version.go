package version

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	// Version application version, can be injected at build time via ldflags
	Version = "dev"

	// GitCommit git commit hash, can be injected at build time via ldflags
	GitCommit = "unknown"

	// BuildTime build timestamp, can be injected at build time via ldflags
	BuildTime = "unknown"

	// GoVersion Go runtime version
	GoVersion = runtime.Version()

	// Website company website
	Website = "www.elasticrelay.com"
)

// Info represents version information structure
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
	Website   string `json:"website"`
}

// Get returns current version information
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
		GoVersion: GoVersion,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Website:   Website,
	}
}

// String returns formatted version information string
func String() string {
	info := Get()
	return fmt.Sprintf("ElasticRelay %s (commit: %s, built: %s, go: %s, platform: %s)",
		info.Version,
		info.GitCommit,
		info.BuildTime,
		info.GoVersion,
		info.Platform,
	)
}

// ANSI color codes
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorBold   = "\033[1m"
)

// DisplayLogo displays the program startup logo
func DisplayLogo() {
	logo := `
` + ColorCyan + ColorBold + `
 ███████╗██╗      █████╗ ███████╗████████╗██╗ ██████╗
 ██╔════╝██║     ██╔══██╗██╔════╝╚══██╔══╝██║██╔════╝
 █████╗  ██║     ███████║███████╗   ██║   ██║██║     
 ██╔══╝  ██║     ██╔══██║╚════██║   ██║   ██║██║     
 ███████╗███████╗██║  ██║███████║   ██║   ██║╚██████╗
 ╚══════╝╚══════╝╚═╝  ╚═╝╚══════╝   ╚═╝   ╚═╝ ╚═════╝
` + ColorReset + `
` + ColorYellow + `
 ██████╗ ███████╗██╗      █████╗ ██╗   ██╗
 ██╔══██╗██╔════╝██║     ██╔══██╗╚██╗ ██╔╝
 ██████╔╝█████╗  ██║     ███████║ ╚████╔╝ 
 ██╔══██╗██╔══╝  ██║     ██╔══██║  ╚██╔╝  
 ██║  ██║███████╗███████╗██║  ██║   ██║   
 ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝   ╚═╝   
` + ColorReset

	// Display logo
	fmt.Println(logo)

	// Display banner
	fmt.Printf("%s%s%s\n", ColorGreen, strings.Repeat("=", 60), ColorReset)
	fmt.Printf("%s%s        Real-time Data Pipeline & CDC Solution        %s%s\n",
		ColorGreen, ColorBold, ColorReset, ColorGreen)
	fmt.Printf("%s%s%s\n\n", ColorGreen, strings.Repeat("=", 60), ColorReset)

	// Display version information
	info := Get()
	fmt.Printf("%s🚀 Version:    %s%s%s\n", ColorWhite, ColorCyan, info.Version, ColorReset)
	fmt.Printf("%s📝 Commit:     %s%.8s%s\n", ColorWhite, ColorYellow, info.GitCommit, ColorReset)
	fmt.Printf("%s⏰ Built:      %s%s%s\n", ColorWhite, ColorPurple, info.BuildTime, ColorReset)
	fmt.Printf("%s🔧 Go Version: %s%s%s\n", ColorWhite, ColorBlue, info.GoVersion, ColorReset)
	fmt.Printf("%s💻 Platform:   %s%s%s\n", ColorWhite, ColorGreen, info.Platform, ColorReset)
	fmt.Printf("%s🌐 Website:    %s%s%s\n", ColorWhite, ColorCyan, info.Website, ColorReset)

	// Display copyright information
	fmt.Printf("%s%s%s\n", ColorGreen, strings.Repeat("-", 70), ColorReset)
	fmt.Printf("%s%s© 2024-2026 Yogoo Software Co., Ltd. - All Rights Reserved%s%s\n",
		ColorGreen, ColorBold, ColorReset, ColorGreen)
	fmt.Printf("%s%s%s\n\n", ColorGreen, strings.Repeat("-", 70), ColorReset)
}
