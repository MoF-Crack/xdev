package env

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Round 15 守卫：PowerShell profile 必须定位到「文档」的**真实**位置。
//
// 真实语料（本机）：`[Environment]::GetFolderPath('MyDocuments')` =
// `D:\documents`，真实 $PROFILE = D:\documents\WindowsPowerShell\...
// （87 字节、无 xdev 块）；而 xdev 把块写进 C:\Users\<user>\Documents\...
// （118 字节、有 xdev 块）。后果：init/activate/doctor 三处都报 ✔，用户 shell
// 却从未加载 XMDEV_HOME 与 cd hook。
//
// 判据：documentsDir() 不得只是「USERPROFILE\Documents」——在文档目录被
// 重定向的机器上必须跟着走。
func TestR15_DocumentsDirFollowsRedirectedKnownFolder(t *testing.T) {
	got := documentsDir()
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("documentsDir() = %q, want an absolute path", got)
	}

	home := homeDirForTest()
	if runtime.GOOS != "windows" {
		if home != "" {
			want := filepath.Join(home, "Documents")
			if !sameDirFold(got, want) {
				t.Fatalf("documentsDir() = %q, want %q", got, want)
			}
		}
		return
	}

	// Windows：注册表里若登记了重定向后的「文档」，必须与之一致。
	registered := registeredDocumentsForTest()
	if registered == "" {
		t.Skip(`User Shell Folders\Personal not set; nothing to compare`)
	}
	if !sameDirFold(got, registered) {
		t.Fatalf("documentsDir() = %q, 但 Windows 登记的「文档」是 %q —— profile 会写到 shell 不加载的位置", got, registered)
	}
	// 反向守卫：重定向时绝不能退化成 USERPROFILE\Documents。
	naive := filepath.Join(home, "Documents")
	if !sameDirFold(registered, naive) && sameDirFold(got, naive) {
		t.Fatalf("文档目录已重定向到 %q，但 documentsDir() 仍返回未重定向的 %q", registered, naive)
	}
}

// profile 路径必须落在 documentsDir() 之下（防止两个推导再次漂移）。
func TestR15_PowerShellProfileLivesUnderDocumentsDir(t *testing.T) {
	docs := filepath.Clean(documentsDir())
	for _, prof := range []string{pwshProfile(), psProfile()} {
		rel, err := filepath.Rel(docs, prof)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("profile %q 不在 documentsDir()=%q 之下", prof, docs)
		}
		if filepath.Base(prof) != "Microsoft.PowerShell_profile.ps1" {
			t.Errorf("profile %q 文件名异常", prof)
		}
	}
}

func homeDirForTest() string {
	if h := os.Getenv("USERPROFILE"); h != "" {
		return h
	}
	return os.Getenv("HOME")
}

func sameDirFold(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
