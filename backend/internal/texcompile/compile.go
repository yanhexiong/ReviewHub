package texcompile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
)

type Config struct {
	Engine          string
	BuildTool       string
	OutputDirectory string
	TexLiveBinPath  string
	ShellEscape     bool
}

type Result struct {
	PDFPath string
}

func Compile(ctx context.Context, workspace, texRoot string, config Config) (Result, error) {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(texRoot) == "" {
		return Result{}, domain.NewAPIError(400, "VALIDATION_ERROR", "TeX 编译参数无效")
	}
	texRoot = filepath.Clean(filepath.FromSlash(texRoot))
	if filepath.IsAbs(texRoot) || texRoot == "." || texRoot == ".." || strings.HasPrefix(texRoot, ".."+string(filepath.Separator)) {
		return Result{}, domain.NewAPIError(400, "TEX_ROOT_NOT_FOUND", "TeX 入口文件不存在于仓库中")
	}
	texPath := filepath.Join(workspace, texRoot)
	if _, err := os.Stat(texPath); err != nil {
		return Result{}, domain.NewAPIError(400, "TEX_ROOT_NOT_FOUND", "TeX 入口文件不存在于仓库中")
	}
	outDir := strings.TrimSpace(config.OutputDirectory)
	if outDir == "" {
		outDir = "build"
	}
	outDir = filepath.Clean(filepath.FromSlash(outDir))
	if filepath.IsAbs(outDir) || outDir == "." || outDir == ".." || strings.HasPrefix(outDir, ".."+string(filepath.Separator)) {
		return Result{}, domain.NewAPIError(400, "VALIDATION_ERROR", "TeX 输出目录无效")
	}
	if err := os.MkdirAll(filepath.Join(workspace, outDir), 0o700); err != nil {
		return Result{}, err
	}

	engine := config.Engine
	if engine != "pdflatex" && engine != "xelatex" && engine != "lualatex" && engine != "tectonic" {
		return Result{}, domain.NewAPIError(400, "VALIDATION_ERROR", "TeX 编译引擎无效")
	}
	buildTool := config.BuildTool
	if buildTool != "latexmk" && buildTool != "tectonic" && buildTool != "engine" {
		return Result{}, domain.NewAPIError(400, "VALIDATION_ERROR", "TeX 编译工具无效")
	}
	binPath := strings.TrimSpace(config.TexLiveBinPath)
	command, args, passes, err := resolveCommand(engine, buildTool, binPath, outDir, texRoot, config.ShellEscape)
	if err != nil {
		return Result{}, err
	}
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null")
	compileContext, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	for pass := 0; pass < passes; pass++ {
		process := exec.CommandContext(compileContext, command, args...)
		process.Dir = workspace
		process.Env = withBinPath(environment, binPath)
		output, err := process.CombinedOutput()
		if err != nil {
			if errors.Is(compileContext.Err(), context.DeadlineExceeded) {
				return Result{}, domain.NewAPIError(400, "TEX_COMPILE_TIMEOUT", "TeX 编译超过 120 秒限制")
			}
			if errors.Is(err, exec.ErrNotFound) {
				return Result{}, domain.NewAPIError(400, "TEX_TOOL_NOT_FOUND", fmt.Sprintf("找不到 TeX 编译程序（%s），请配置有效的 TeX bin 目录", command))
			}
			detail := strings.TrimSpace(string(output))
			if len(detail) > 1500 {
				detail = detail[len(detail)-1500:]
			}
			return Result{}, domain.NewAPIError(400, "TEX_COMPILE_FAILED", "TeX 编译失败："+detail)
		}
	}
	base := strings.TrimSuffix(filepath.Base(texRoot), filepath.Ext(texRoot))
	candidates := []string{filepath.Join(workspace, outDir, base+".pdf"), filepath.Join(workspace, base+".pdf")}
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
			return Result{PDFPath: candidate}, nil
		}
	}
	return Result{}, domain.NewAPIError(400, "TEX_PDF_NOT_FOUND", "TeX 编译结束但未找到生成的 PDF")
}

func resolveCommand(engine, buildTool, binPath, outDir, texRoot string, shellEscape bool) (string, []string, int, error) {
	if buildTool == "tectonic" || (buildTool == "latexmk" && engine == "tectonic") {
		command, err := findExecutable("tectonic", binPath)
		if err != nil {
			return "", nil, 0, domain.NewAPIError(400, "TEX_TOOL_NOT_FOUND", "未找到 Tectonic，请配置有效的 TeX bin 目录")
		}
		return command, []string{"-X", "compile", "--outdir=" + outDir, texRoot}, 1, nil
	}
	if buildTool == "latexmk" {
		if command, err := findExecutable("latexmk", binPath); err == nil {
			flag := "-pdf"
			if engine == "xelatex" {
				flag = "-xelatex"
			} else if engine == "lualatex" {
				flag = "-lualatex"
			}
			args := []string{flag, "-interaction=nonstopmode", "-halt-on-error", "-file-line-error", "-outdir=" + outDir}
			if shellEscape {
				args = append(args, "-shell-escape")
			}
			return command, append(args, texRoot), 1, nil
		}
	}
	command, err := findExecutable(engine, binPath)
	if err != nil {
		return "", nil, 0, domain.NewAPIError(400, "TEX_TOOL_NOT_FOUND", fmt.Sprintf("未找到 %s，请配置有效的 TeX bin 目录", engine))
	}
	args := []string{"-interaction=nonstopmode", "-halt-on-error", "-file-line-error", "-output-directory=" + outDir}
	if shellEscape {
		args = append(args, "-shell-escape")
	}
	return command, append(args, texRoot), 2, nil
}

func findExecutable(name, binPath string) (string, error) {
	if binPath != "" {
		candidate := filepath.Join(binPath, name)
		if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return exec.LookPath(name)
}

func withBinPath(environment []string, binPath string) []string {
	if binPath == "" {
		return environment
	}
	pathValue := binPath + string(os.PathListSeparator) + os.Getenv("PATH")
	result := make([]string, 0, len(environment)+1)
	pathSet := false
	for _, value := range environment {
		if strings.HasPrefix(value, "PATH=") {
			result = append(result, "PATH="+pathValue)
			pathSet = true
		} else {
			result = append(result, value)
		}
	}
	if !pathSet {
		result = append(result, "PATH="+pathValue)
	}
	return result
}
