package importer

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/stevetosak/git-idm/internal/config"
	"gopkg.in/yaml.v3"
)

func sshKeyFromCommand(sshCommand string) (key, source string) {
	fields := strings.Fields(sshCommand)
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "-i":
			return fields[i+1], "core.sshCommand -i"
		case "-F":
			cfg := fields[i+1]
			if k := identityFileFromSSHConfig(config.ExpandHome(cfg)); k != "" {
				return k, "core.sshCommand -F " + cfg
			}
		}
	}
	return "", ""
}

func identityFileFromSSHConfig(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.EqualFold(fields[0], "IdentityFile") {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

func ghConfigDirFromEnvrc(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "export ")
		value, ok := strings.CutPrefix(line, "GH_CONFIG_DIR=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		home, _ := os.UserHomeDir()
		value = strings.ReplaceAll(value, "${HOME}", home)
		value = strings.ReplaceAll(value, "$HOME", home)
		return config.ContractHome(config.ExpandHome(value))
	}
	return ""
}

func envrcOnlySetsGHConfigDir(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "GH_CONFIG_DIR=") {
			return false
		}
	}
	return true
}

func DefaultGHConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return config.ContractHome(filepath.Join(xdg, "gh"))
	}
	return "~/.config/gh"
}

func GHUserFromConfigDir(dir string) string {
	data, err := os.ReadFile(filepath.Join(config.ExpandHome(dir), "hosts.yml"))
	if err != nil {
		return ""
	}
	var hosts map[string]struct {
		User string `yaml:"user"`
	}
	if err := yaml.Unmarshal(data, &hosts); err != nil {
		return ""
	}
	return hosts["github.com"].User
}
