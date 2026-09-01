package sandbox

import (
	"os"
	"path"
	"path/filepath"
)

// defaultSecretExcludes lists the workspace-relative paths shadowed out of
// every container mount regardless of ExecOpts (P81.10/FIND-10). ".aegis/.env"
// is a secrets file that lives inside the mounted workspace directory
// (internal/config's own doc comment names it a deliberate hole for the
// operator's own tooling, not for a sandboxed command); ContainerOpts.SecretExcludes
// extends this list.
var defaultSecretExcludes = []string{".aegis/.env"}

// mountModeSuffix returns ":ro" when opts asks for a read-only workspace
// mount, or "" for the default read-write mount.
func mountModeSuffix(opts ExecOpts) string {
	if opts.ReadOnly {
		return ":ro"
	}
	return ""
}

// secretShadowArgs returns the "-v"/source/target pairs that shadow this
// backend's secretExcludes out of a container mounting dir, converting each
// shadow source's host path through toMountHost (the same per-runtime
// conversion the caller uses for the workspace mount itself: hostPathForMount,
// wslHostPath, or the identity function for Apple Containers). A path that
// does not exist under dir is skipped — there is nothing to shadow.
func (c *ContainerBackend) secretShadowArgs(dir string, toMountHost func(string) string) []string {
	if dir == "" {
		return nil
	}
	var args []string
	for _, rel := range c.secretExcludes {
		hostAbs := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Stat(hostAbs)
		if err != nil {
			continue
		}
		shadow, serr := c.shadowSource(info.IsDir())
		if serr != nil {
			if c.logger != nil {
				c.logger.Warn("sandbox: could not create shadow mount for secret exclusion", "path", rel, "err", serr)
			}
			continue
		}
		containerPath := path.Join("/workspace", filepath.ToSlash(rel))
		args = append(args, "-v", toMountHost(shadow)+":"+containerPath+":ro")
	}
	return args
}

// shadowSource lazily creates (once per backend) an empty file and an empty
// directory on the host to serve as bind-mount sources that shadow a secret
// path out of the container — a bind mount whose source is empty and
// read-only, layered over the same target the workspace mount also covers.
func (c *ContainerBackend) shadowSource(isDir bool) (string, error) {
	c.shadowMu.Lock()
	defer c.shadowMu.Unlock()
	if isDir {
		if c.shadowDirPath != "" {
			return c.shadowDirPath, nil
		}
		dir, err := os.MkdirTemp("", "aegis-sandbox-shadow-dir-")
		if err != nil {
			return "", err
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			return "", err
		}
		c.shadowDirPath = dir
		return dir, nil
	}
	if c.shadowFilePath != "" {
		return c.shadowFilePath, nil
	}
	f, err := os.CreateTemp("", "aegis-sandbox-shadow-file-")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	if err := os.Chmod(path, 0o400); err != nil {
		return "", err
	}
	c.shadowFilePath = path
	return path, nil
}

// closeShadowMounts removes the temp file/dir created by shadowSource, if any.
func (c *ContainerBackend) closeShadowMounts() {
	c.shadowMu.Lock()
	defer c.shadowMu.Unlock()
	if c.shadowFilePath != "" {
		os.Remove(c.shadowFilePath)
		c.shadowFilePath = ""
	}
	if c.shadowDirPath != "" {
		os.RemoveAll(c.shadowDirPath)
		c.shadowDirPath = ""
	}
}
