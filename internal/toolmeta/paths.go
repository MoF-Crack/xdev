package toolmeta

import (
	"os"
	"path/filepath"
	"runtime"
)

// CommonInstallPaths 返回工具在当前平台的常见安装路径（gui 版超集原样移入）。
// 覆盖 env/migrate.go 与 gui/app_local_detect.go 两处重复实现；env 侧
// 迁移后为超集（多出 mingw/typescript/darwin/linux 及 nvm/pyenv/sdkman 探测）。
func CommonInstallPaths(name string) []string {
	var paths []string
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")

		switch name {
		case "python", "python3":
			paths = append(paths,
				filepath.Join(localAppData, "Programs", "Python"),
			)
		case "node":
			paths = append(paths,
				filepath.Join(programFiles, "nodejs"),
				filepath.Join(localAppData, "nvm"),
			)
		case "go":
			paths = append(paths,
				filepath.Join(programFiles, "Go"),
				filepath.Join(home, "go"),
				filepath.Join(home, "sdk", "go"),
			)
		case "java":
			paths = append(paths,
				filepath.Join(programFiles, "Java"),
				filepath.Join(programFilesX86, "Java"),
			)
		case "dotnet":
			paths = append(paths,
				filepath.Join(programFiles, "dotnet"),
			)
		case "ruby":
			paths = append(paths,
				filepath.Join(programFiles, "Ruby"),
			)
		case "git":
			paths = append(paths,
				filepath.Join(programFiles, "Git"),
			)
		case "mingw":
			paths = append(paths,
				filepath.Join(localAppData, "Programs", "mingw64"),
				filepath.Join(localAppData, "Programs", "mingw32"),
				filepath.Join(programFiles, "mingw64"),
				filepath.Join(programFiles, "mingw32"),
				filepath.Join(home, "scoop", "apps", "mingw"),
				filepath.Join("C:", "msys64", "mingw64"),
				filepath.Join("C:", "msys64", "mingw32"),
			)
		case "flutter":
			paths = append(paths,
				filepath.Join(localAppData, "flutter"),
			)
		case "android-platform-tools":
			paths = append(paths,
				filepath.Join(home, "AppData", "Local", "Android", "Sdk", "platform-tools"),
			)
		case "typescript":
			// npm global install paths
			paths = append(paths,
				filepath.Join(localAppData, "npm", "node_modules", "typescript", "bin"),
				filepath.Join(programFiles, "nodejs", "node_modules", "typescript", "bin"),
			)
		}
	} else if runtime.GOOS == "darwin" {
		switch name {
		case "python", "python3":
			paths = append(paths,
				"/usr/local/bin/python3",
				"/opt/homebrew/bin/python3",
			)
		case "node":
			paths = append(paths,
				"/usr/local/bin/node",
				"/opt/homebrew/bin/node",
			)
		case "go":
			paths = append(paths,
				"/usr/local/go/bin/go",
				"/opt/homebrew/bin/go",
			)
		case "java":
			paths = append(paths,
				"/usr/local/opt/openjdk/bin/java",
				"/Library/Java/JavaVirtualMachines",
			)
		case "flutter":
			paths = append(paths,
				filepath.Join(home, "development", "flutter"),
			)
		case "typescript":
			paths = append(paths,
				"/usr/local/lib/node_modules/typescript/bin",
				"/opt/homebrew/lib/node_modules/typescript/bin",
			)
		}
	} else {
		// Linux
		switch name {
		case "python", "python3":
			paths = append(paths, "/usr/bin/python3", "/usr/local/bin/python3")
		case "node":
			paths = append(paths, "/usr/bin/node", "/usr/local/bin/node")
		case "go":
			paths = append(paths, "/usr/local/go/bin/go", "/usr/bin/go")
		case "java":
			paths = append(paths, "/usr/bin/java", "/usr/local/bin/java")
		case "typescript":
			paths = append(paths,
				"/usr/local/lib/node_modules/typescript/bin",
				"/usr/lib/node_modules/typescript/bin",
			)
		}
	}

	// NVM / pyenv / sdkman etc.
	switch name {
	case "node":
		nvmDir := filepath.Join(home, ".nvm")
		if _, err := os.Stat(nvmDir); err == nil {
			paths = append(paths, nvmDir)
		}
	case "python", "python3":
		pyenvDir := filepath.Join(home, ".pyenv")
		if _, err := os.Stat(pyenvDir); err == nil {
			paths = append(paths, pyenvDir)
		}
	case "java":
		sdkmanDir := filepath.Join(home, ".sdkman")
		if _, err := os.Stat(sdkmanDir); err == nil {
			paths = append(paths, sdkmanDir)
		}
	}

	return paths
}

// CheckEnvVars 检查工具关联的知名环境变量并返回工具二进制路径（找到则为非空）。
// 查 toolmeta 表（EnvVarKeys / BinNames），不再硬编码 envMap。
// android-* 子目录特判逻辑与 gui/app_local_detect.go 原 checkEnvVars 一致。
func CheckEnvVars(name string) string {
	envVarKeys := EnvVarKeys(name)
	if len(envVarKeys) == 0 {
		return ""
	}

	binNames := BinNames(name)
	for _, envVar := range envVarKeys {
		val := os.Getenv(envVar)
		if val == "" {
			continue
		}
		// Try to find the binary within the env var directory
		for _, bin := range binNames {
			candidate := filepath.Join(val, "bin", bin)
			if runtime.GOOS == "windows" {
				candidateExe := candidate + ".exe"
				if _, err := os.Stat(candidateExe); err == nil {
					return candidateExe
				}
			}
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			// Also try directly in the env var directory
			candidate = filepath.Join(val, bin)
			if runtime.GOOS == "windows" {
				candidateExe := candidate + ".exe"
				if _, err := os.Stat(candidateExe); err == nil {
					return candidateExe
				}
			}
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		// For Android tools, check subdirectories
		if name == "android-platform-tools" {
			candidate := filepath.Join(val, "platform-tools")
			if runtime.GOOS == "windows" {
				if _, err := os.Stat(filepath.Join(candidate, "adb.exe")); err == nil {
					return filepath.Join(candidate, "adb.exe")
				}
			} else {
				if _, err := os.Stat(filepath.Join(candidate, "adb")); err == nil {
					return filepath.Join(candidate, "adb")
				}
			}
		}
		if name == "android-build-tools" {
			candidate := filepath.Join(val, "build-tools")
			if entries, err := os.ReadDir(candidate); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						return filepath.Join(candidate, e.Name())
					}
				}
			}
		}
		if name == "android-ndk" {
			candidate := filepath.Join(val, "ndk")
			if entries, err := os.ReadDir(candidate); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						return filepath.Join(candidate, e.Name())
					}
				}
			}
			// Also check ANDROID_NDK_HOME directly
			if fi, err := os.Stat(val); err == nil && fi.IsDir() {
				return val
			}
		}
		if name == "android-emulator" {
			candidate := filepath.Join(val, "emulator")
			if runtime.GOOS == "windows" {
				if _, err := os.Stat(filepath.Join(candidate, "emulator.exe")); err == nil {
					return filepath.Join(candidate, "emulator.exe")
				}
			} else {
				if _, err := os.Stat(filepath.Join(candidate, "emulator")); err == nil {
					return filepath.Join(candidate, "emulator")
				}
			}
		}
	}
	return ""
}