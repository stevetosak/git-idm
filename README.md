# git-idm

One git identity per folder: commit author, SSH key, signing key and GitHub (`gh`) login.

Work repos commit as your work email, push with your work SSH key and use your work `gh` account.
Personal repos do the same with your personal account. No manual switching.

```
~/Projects/work/api      → work      (you@company.com, ~/.ssh/id_work)
~/Projects/personal/blog → personal  (you@gmail.com,   ~/.ssh/id_personal)
```

## Install

macOS and Linux, amd64 and arm64:

```sh
curl -fsSL https://raw.githubusercontent.com/stevetosak/git-idm/main/install.sh | sh
```

The script checks the SHA-256 of the download and installs to `~/.local/bin`.

| Variable | Use |
|---|---|
| `GIT_IDM_VERSION=v0.1.0` | install one version, not the latest |
| `GIT_IDM_INSTALL_DIR=/usr/local/bin` | install to another folder |

With Go: `go install github.com/stevetosak/git-idm/cmd/git-idm@latest`

**Needs:** git 2.13 or later. Optional: [gh](https://cli.github.com) for GitHub logins, zsh, bash or fish for the shell hook.

## Quick start

```sh
git-idm init                      # import your git, SSH and gh setup and repo authors
git-idm add                       # or create a new identity, step by step
git-idm bind work ~/Projects/work # use "work" for every repo in this folder
git-idm bind                      # bind this repo to the default identity
git-idm                           # show the identity for this folder
git-idm doctor                    # find problems
```

Add the shell hook for the correct `gh` account after `cd`, `git-idm use` and the prompt segment:

```sh
echo 'eval "$(git-idm hook zsh)"' >> ~/.zshrc     # zsh
echo 'eval "$(git-idm hook bash)"' >> ~/.bashrc   # bash
echo 'git-idm hook fish | source' >> ~/.config/fish/config.fish   # fish
```

## Commands

| Command | What it does |
|---|---|
| `add`, `edit`, `rm`, `ls` | create, change, remove and list identities |
| `bind` | bind this repo (or folder) to the default identity, or pick one from a list |
| `bind <identity> [dir]` | use an identity for every repo in a folder |
| `bind <identity> [dir] --temp` | use it for a short time, until `unbind` |
| `unbind [dir]` | stop using an identity in a folder |
| `use <identity>` | switch identity in this shell only |
| `doctor` | check keys, gh logins, tokens, hooks, the shell and repo configs |
| `login [identity]` | log in to GitHub again when a token stops working |
| `guard block\|warn\|off` | stop or warn about commits and pushes with the wrong identity |
| `allow` | accept the author that one repo sets itself |
| `init`, `migrate` | import your old setup and repo authors, then remove them |
| `update` | update to the latest release |

Run `git-idm <command> --help` for flags and examples.

## How it works

- **Config:** identities and folder rules are in `~/.config/git-idm/config.toml`.
- **Bound folders only:** the default identity is never used by itself. With the guard on, git refuses to commit in a folder without a rule, pushes to GitHub stop and gh is refused. `git-idm bind` binds the current repo to the default identity.
- **git:** `git-idm apply` writes one marked block in `~/.gitconfig` with an `includeIf "gitdir:"` rule for each folder (`gitdir/i:` on macOS). Each rule loads a generated file that sets `user.name`, `user.email`, `core.sshCommand`, the signing key, a credential helper that gives git only the identity's GitHub token (or stops git), and an SSH command that checks the GitHub account of the key before each push. git does the switching itself.
- **gh:** each identity has its own `GH_CONFIG_DIR`. The shell hook sets it when you change folders, so `gh` uses the correct account. Programs that do not run the hook (scripts, editors, AI agents) get the same account through a `gh` shim in `~/.local/bin`, which `apply` installs. In a folder with an identity, the shim refuses every gh command (except `auth`, `config`, `help` and `version`) when gh is logged in to another account, when the identity has no gh login, or when `GH_TOKEN` or `GITHUB_TOKEN` is set. Skip the check once with `GIT_IDM_GH_CHECK=0`.
- **Guard:** with `guard block`, a global pre-commit hook compares the commit author and committer with the identity of the folder. A global pre-push hook checks every commit that no remote has yet, and stops pushes that skip the git-idm account checks (a token in the remote URL, `GIT_SSH_COMMAND`, or a credential helper or `core.sshCommand` given with `-c`). Your repo hooks still run after it.

## Updates

git-idm checks for a new release once a day in the background and shows a short notice with the release notes.

```sh
git-idm update          # download, check the checksum, replace the binary
git-idm update --check  # only show what is new
```

Set `GIT_IDM_NO_UPDATE_CHECK=1` to turn off the daily check.

## Uninstall

```sh
rm ~/.local/bin/git-idm
rm ~/.local/bin/gh        # the gh shim, only if git-idm made it
rm -rf ~/.config/git-idm
```

Then remove the block between the `# >>> git-idm` and `# <<< git-idm` lines in `~/.gitconfig`, and the hook line from `~/.zshrc`.

## License

[MIT](LICENSE)
