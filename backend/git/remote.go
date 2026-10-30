package git

// 远端同步：拉取、推送、取回，以及远端登记。
// 失败原因几乎都在网络与凭据上，话术要指向「检查网络或权限」，
// 而不是「检查文件冲突」——那两件事用户能做的处置完全不同。

import (
	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
	"strings"
	"time"
)

// pushTimeout 推送超时：远端慢时一分钟都算客气，超时报错让用户自己重试。
const pushTimeout = 120 * time.Second

func Pull(dir string, remote, branch string, rebase bool) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	args := []string{"pull", "--no-edit"}
	if rebase {
		args = []string{"pull", "--rebase"}
	}
	if r := strings.TrimSpace(remote); r != "" {
		if !validRemoteName(r) {
			return nil, pkg.New(pkg.CodeGitBadInput, "远端名不合法", r)
		}
		args = append(args, r)
		if b := strings.TrimSpace(branch); b != "" {
			if !validBranchName(b) {
				return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", b)
			}
			args = append(args, b)
		}
	}
	// 拉取过程中禁止 git 弹出交互式提问（凭据 / 编辑器），否则界面会一直等。
	if _, err := writeRunEnv(sc.root, pushTimeout, gitNonInteractive(), args...); err != nil {
		st, serr := Status(dir)
		if serr == nil && st.Conflict {
			return st, nil
		}
		return nil, pushFailure(err)
	}
	return Status(dir)
}

// Push 推送当前分支。第一次推送要建立上游；强制推送分级，纯 --force 必须输分支名确认。
func Push(dir string, req domain.GitPushREQ) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	cur := sc.branch()
	if cur == "" {
		return nil, pkg.New(pkg.CodeGitBadInput, "还没有提交，先提交一次再推送", "")
	}
	remoteName := strings.TrimSpace(req.RemoteName)
	branchName := strings.TrimSpace(req.BranchName)
	if branchName == "" {
		branchName = cur
	}
	if !validBranchName(branchName) {
		return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", branchName)
	}
	if remoteName != "" && !validRemoteName(remoteName) {
		return nil, pkg.New(pkg.CodeGitBadInput, "远端名不合法", remoteName)
	}
	// 没指定远端时按上游推断；再没有就只认唯一远端，多于一个必须让用户选。
	if remoteName == "" {
		if up := sc.upstream(); up != "" {
			remoteName, _ = splitUpstream(up)
		} else {
			remotes := sc.remoteNames()
			switch len(remotes) {
			case 1:
				remoteName = remotes[0]
			case 0:
				return nil, pkg.New(pkg.CodeNoRemote, "这个仓库还没有配置远端",
					"先在设置里添加一个远端地址（git remote add origin <url>）")
			default:
				return nil, pkg.New(pkg.CodeRemoteAmbiguous, "这个仓库有多个远端，请选择推送目标", strings.Join(remotes, ", "))
			}
		}
	}

	args := []string{"push"}
	switch strings.TrimSpace(req.Force) {
	case "", domain.GitForceNone:
	case domain.GitForceLease:
		// 故意不先 fetch：租约比的是本地远端跟踪引用，那个引用只在上一次同步时更新过。
		// 先 fetch 会把租约变成「无条件覆盖」——租约的全部价值就在于「我上次同步之后没人推过」。
		args = append(args, "--force-with-lease")
	case domain.GitForceTrue:
		// 无条件强制推送会静默覆盖别人的提交，必须输入分支名确认。
		if strings.TrimSpace(req.Confirm) != branchName {
			return nil, pkg.New(pkg.CodeGitBadInput, "强制推送需要输入分支名确认", "请输入 "+branchName)
		}
		args = append(args, "--force")
	default:
		return nil, pkg.New(pkg.CodeGitBadInput, "不认识的强制推送级别", req.Force)
	}

	// 没上游或显式要求时建立跟踪，否则第一次推送永远会被 git 拒。
	if req.SetUpstream || sc.upstream() == "" {
		args = append(args, "--set-upstream")
	}
	args = append(args, remoteName, branchName)

	env := gitNonInteractive()
	if _, err := writeRunEnv(sc.root, pushTimeout, env, args...); err != nil {
		return nil, pushFailure(err)
	}
	return Status(dir)
}

// Fetch 取回远端的最新状态，不改工作区也不改本地分支。
// 界面需要它：推送被拒之后，用户得先看见远端多了什么才能决定拉取还是覆盖。
func Fetch(dir, remote string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(remote)
	args := []string{"fetch", "--prune"}
	if target != "" {
		if !validRemoteName(target) {
			return nil, pkg.New(pkg.CodeGitBadInput, "远端名不合法", target)
		}
		args = append(args, target)
	} else if remotes := sc.remoteNames(); len(remotes) == 1 {
		args = append(args, remotes[0])
	}
	if _, err := writeRunEnv(sc.root, pushTimeout, gitNonInteractive(), args...); err != nil {
		return nil, pushFailure(err)
	}
	return Status(dir)
}

// gitNonInteractive 让 git 在缺凭据时直接失败而不是等终端输入：
// GUI 里没有终端，等下去的结局是界面卡到超时。
func gitNonInteractive() []string {
	return []string{"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=:", "GIT_SEQUENCE_EDITOR=:", "GCM_INTERACTIVE=never"}
}

// pushFailure 把推送失败翻成可执行的下一步；凭据与网络问题各给不同的处置。
func pushFailure(err error) error {
	text := strings.ToLower(errText(err))
	switch {
	case strings.Contains(text, "non-fast-forward"), strings.Contains(text, "fetch first"),
		strings.Contains(text, "updates were rejected"):
		return pkg.New(pkg.CodePushRejected, "远端有本地没有的提交，推送被拒绝",
			"先拉取合并；确认要覆盖远端时用强制推送（会改写远端历史）")
	case strings.Contains(text, "stale info"):
		return pkg.New(pkg.CodeLeaseRejected, "远端在你上次同步之后又变了，带租约的强制推送已拒绝",
			"先「取回远端」看清楚别人推了什么；确认之后再用无条件强制推送覆盖")
	case strings.Contains(text, "no upstream branch"):
		return pkg.New(pkg.CodeRemoteAmbiguous, "当前分支还没有上游分支", "推送时勾选「建立上游」")
	case strings.Contains(text, "authentication failed"), strings.Contains(text, "could not read username"),
		strings.Contains(text, "terminal prompts disabled"), strings.Contains(text, "permission denied"):
		return pkg.New(pkg.CodePushAuth, "远端拒绝了身份验证",
			"这个仓库需要凭据（HTTPS 令牌或 SSH 密钥），请在自己的终端里配置一次")
	case strings.Contains(text, "could not resolve host"), strings.Contains(text, "network"),
		strings.Contains(text, "connection refused"), strings.Contains(text, "timed out"):
		return pkg.New(pkg.CodePushNetwork, "连不上远端，检查网络或远端地址", errText(err))
	case strings.Contains(text, "no configured push destination"), strings.Contains(text, "no remote"):
		return pkg.New(pkg.CodeNoRemote, "这个仓库还没有配置远端", "请先添加一个远端地址")
	}
	return err
}

// Init 在当前目录初始化仓库；已存在仓库时不做任何事（不覆盖用户的分支配置）。
func Init(dir string, branch string) (*domain.GitStatusVO, error) {
	if !Available() {
		return nil, pkg.New(pkg.CodeGitUnavailable, "这台电脑上还没有 Git，装好后才能初始化仓库", "")
	}
	if !pkg.DirExists(dir) {
		return nil, pkg.New(pkg.CodeWorkspaceInvalid, "工作目录不存在", dir)
	}
	args := []string{"init"}
	if b := strings.TrimSpace(branch); b != "" {
		if !validBranchName(b) {
			return nil, pkg.New(pkg.CodeGitBadInput, "分支名不合法", b)
		}
		args = append(args, "--initial-branch="+b)
	}
	if _, err := writeRun(dir, writeTimeout, args...); err != nil {
		return nil, err
	}
	return Status(dir)
}

// AddRemote 添加一个远端地址。
func AddRemote(dir, name, url string) (*domain.GitStatusVO, error) {
	sc, err := requireScope(dir)
	if err != nil {
		return nil, err
	}
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if !validRemoteName(name) {
		return nil, pkg.New(pkg.CodeGitBadInput, "远端名不合法", "只允许字母、数字与 . _ -")
	}
	if !validRemoteURL(url) {
		return nil, pkg.New(pkg.CodeGitBadInput, "远端地址不合法", "支持 https:// / ssh:// / git@host:path / 本地目录")
	}
	if _, err := writeRun(sc.root, writeTimeout, "remote", "add", name, url); err != nil {
		return nil, pkg.New(pkg.CodeGitRejected, "添加远端失败", errText(err)+"\n同名远端可能已存在")
	}
	return Status(dir)
}

// headHash 取当前 HEAD 的完整 hash，供提交后跳到历史页那一条。
func (s *scope) headHash() string {
	res, err := run(s.root, statusTimeout, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.stdout)
}
