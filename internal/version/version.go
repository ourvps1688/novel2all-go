// Package version 提供 novel2all-go 版本元数据。
// P0 阶段通过 ldflags 注入，P1+ 接入 git tag 自动注入。
package version

// 这些变量在编译时通过 -ldflags "-X github.com/ourvps1688/novel2all-go/internal/version.Version=..." 注入
var (
	// Version 语义化版本，如 "0.1.0"
	Version = "0.1.0"

	// Commit git commit SHA
	Commit = "dev"

	// BuildTime ISO8601 编译时间
	BuildTime = "unknown"

	// GoVersion 编译时的 Go 版本
	GoVersion = "1.22+"
)

// Info 返回完整的版本信息
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// Get 返回当前版本信息
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: GoVersion,
	}
}