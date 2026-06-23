// Command devc-secretfs mounts a passthrough FUSE filesystem over a backing
// directory that HIDES files matching secret patterns from everything reading
// through the mount. devc ships and runs this inside the container so the agent
// works in a filtered view of the workspace while the host keeps full access.
//
// Hiding is dynamic: every lookup/readdir is evaluated live, so files matching a
// pattern are invisible no matter when or where (any subdirectory) they are
// created. Matched entries return ENOENT on lookup and are omitted from readdir.
//
// It mounts with DirectMount (a direct mount(2) syscall, so no `fusermount`
// binary is needed in the image) and AllowOther (so the non-root agent user can
// read a mount created by root). It therefore requires /dev/fuse and
// CAP_SYS_ADMIN, which devc grants to the container only in this mode.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/sxwebdev/devc/internal/secretmatch"
)

func main() {
	backing := flag.String("backing", "", "backing directory (the real workspace)")
	mountpoint := flag.String("mount", "", "mountpoint (the filtered view the agent sees)")
	denyCSV := flag.String("deny", "", "comma-separated secret glob patterns to hide")
	allowCSV := flag.String("allow", "", "comma-separated allow-list glob patterns")
	uid := flag.Int("uid", -1, "report this owner uid for all entries (-1 = passthrough)")
	gid := flag.Int("gid", -1, "report this owner gid for all entries (-1 = passthrough)")
	debug := flag.Bool("debug", false, "enable FUSE debug logging")
	flag.Parse()

	if *backing == "" || *mountpoint == "" {
		log.Fatalf("devc-secretfs: --backing and --mount are required")
	}

	matcher := secretmatch.New(splitCSV(*denyCSV), splitCSV(*allowCSV))

	// Present files as owned by the container user (the FUSE daemon runs as root,
	// and Docker's backing mount can report a different owner, which trips git's
	// "dubious ownership" check). The daemon still performs writes as root, so
	// remapping the displayed owner does not restrict the agent.
	var owner *fuse.Owner
	if *uid >= 0 && *gid >= 0 {
		owner = &fuse.Owner{Uid: uint32(*uid), Gid: uint32(*gid)}
	}

	var st syscall.Stat_t
	if err := syscall.Stat(*backing, &st); err != nil {
		log.Fatalf("devc-secretfs: stat backing %q: %v", *backing, err)
	}

	root := &fs.LoopbackRoot{
		Path: *backing,
		Dev:  uint64(st.Dev),
	}
	root.NewNode = func(rootData *fs.LoopbackRoot, _ *fs.Inode, _ string, _ *syscall.Stat_t) fs.InodeEmbedder {
		n := &filterNode{matcher: matcher, owner: owner}
		n.RootData = rootData
		return n
	}
	rootNode := root.NewNode(root, nil, "", &st)
	root.RootNode = rootNode

	opts := &fs.Options{}
	opts.MountOptions.DirectMount = true
	opts.MountOptions.AllowOther = true
	opts.MountOptions.FsName = "devc-secretfs"
	opts.MountOptions.Name = "devc-secretfs"
	opts.MountOptions.Debug = *debug

	server, err := fs.Mount(*mountpoint, rootNode, opts)
	if err != nil {
		log.Fatalf("devc-secretfs: mount %q: %v", *mountpoint, err)
	}

	// Unmount cleanly on termination so the mountpoint isn't left stale.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		_ = server.Unmount()
	}()

	fmt.Fprintf(os.Stderr, "devc-secretfs: mounted %s -> %s (%d deny patterns)\n",
		*backing, *mountpoint, len(splitCSV(*denyCSV)))
	server.Wait()
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// filterNode is a loopback node that hides secret-matching children and, when
// owner is set, reports a fixed owner so host-side ownership quirks don't trip
// tools like git.
type filterNode struct {
	fs.LoopbackNode
	matcher *secretmatch.Matcher
	owner   *fuse.Owner
}

var (
	_ = (fs.NodeLookuper)((*filterNode)(nil))
	_ = (fs.NodeOpendirHandler)((*filterNode)(nil))
	_ = (fs.NodeGetattrer)((*filterNode)(nil))
	_ = (fs.NodeRenamer)((*filterNode)(nil))
	_ = (fs.NodeLinker)((*filterNode)(nil))
)

// rel returns the workspace-relative path of a child named `name` of this node.
func (n *filterNode) rel(name string) string {
	return path.Join(n.EmbeddedInode().Path(nil), name)
}

// Lookup hides matching paths so the agent cannot open or stat them.
func (n *filterNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if n.matcher.Match(n.rel(name)) {
		return nil, syscall.ENOENT
	}
	ch, errno := n.LoopbackNode.Lookup(ctx, name, out)
	if errno == 0 && n.owner != nil {
		out.Owner = *n.owner
	}
	return ch, errno
}

// Getattr reports the fixed owner (if configured) over the backing file's owner.
func (n *filterNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	errno := n.LoopbackNode.Getattr(ctx, f, out)
	if errno == 0 && n.owner != nil {
		out.Owner = *n.owner
	}
	return errno
}

// Rename is denied when it would move a hidden file — or a directory whose
// subtree contains one — out of its matched scope, or target a hidden name.
// Without this, the agent could rename a visible ancestor (e.g. `mv internal x`)
// to escape a path-anchored pattern like `internal/**/*.key` and then read the
// now-unmatched secret. Lookup blocks renaming a matched file directly, but not
// renaming its visible parent — this closes that gap.
func (n *filterNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	srcRel := n.rel(name)
	dstRel := path.Join(newParent.EmbeddedInode().Path(nil), newName)
	if n.matcher.Match(srcRel) || n.matcher.Match(dstRel) || n.subtreeHasSecret(srcRel) {
		return syscall.EACCES
	}
	return n.LoopbackNode.Rename(ctx, name, newParent, newName, flags)
}

// Link refuses to hardlink a hidden source or create a hidden-named link, so a
// matched file cannot be re-exposed under a different (unmatched) path.
func (n *filterNode) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if n.matcher.Match(n.rel(name)) || n.matcher.Match(target.EmbeddedInode().Path(nil)) {
		return nil, syscall.EACCES
	}
	ch, errno := n.LoopbackNode.Link(ctx, target, name, out)
	if errno == 0 && n.owner != nil {
		out.Owner = *n.owner
	}
	return ch, errno
}

// subtreeHasSecret reports whether the backing path for srcRel is a directory
// that contains, at any depth, a file matching a deny pattern. Renaming such a
// directory would relocate hidden files out from under path-anchored patterns,
// so it must be refused.
func (n *filterNode) subtreeHasSecret(srcRel string) bool {
	backing := filepath.Join(n.RootData.Path, filepath.FromSlash(srcRel))
	info, err := os.Lstat(backing)
	if err != nil || !info.IsDir() {
		return false
	}
	found := false
	_ = filepath.WalkDir(backing, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(n.RootData.Path, p)
		if relErr != nil {
			return nil
		}
		if n.matcher.Match(filepath.ToSlash(rel)) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// OpendirHandle drains the real directory stream, drops matching entries, and
// serves the filtered result so the agent never lists a hidden file.
func (n *filterNode) OpendirHandle(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	fh, fuseFlags, errno := n.LoopbackNode.OpendirHandle(ctx, flags)
	if errno != 0 {
		return fh, fuseFlags, errno
	}
	rd, ok := fh.(fs.FileReaddirenter)
	if !ok {
		return fh, fuseFlags, errno
	}

	base := n.EmbeddedInode().Path(nil)
	var entries []fuse.DirEntry
	for {
		de, e := rd.Readdirent(ctx)
		if e != 0 || de == nil {
			break
		}
		if de.Name != "." && de.Name != ".." && n.matcher.Match(path.Join(base, de.Name)) {
			continue
		}
		entries = append(entries, *de)
	}
	if rel, ok := fh.(fs.FileReleasedirer); ok {
		rel.Releasedir(ctx, 0)
	}

	return &sliceDirStream{entries: entries}, fuseFlags, 0
}

// sliceDirStream serves a fixed, pre-filtered list of directory entries. It
// assigns its own sequential offsets so Seekdir (used by the kernel to resume an
// interrupted readdir) lands on the right index after filtering.
type sliceDirStream struct {
	entries []fuse.DirEntry
	off     int
}

var (
	_ = (fs.FileReaddirenter)((*sliceDirStream)(nil))
	_ = (fs.FileSeekdirer)((*sliceDirStream)(nil))
	_ = (fs.FileReleasedirer)((*sliceDirStream)(nil))
)

func (d *sliceDirStream) Readdirent(_ context.Context) (*fuse.DirEntry, syscall.Errno) {
	if d.off >= len(d.entries) {
		return nil, 0
	}
	e := d.entries[d.off]
	d.off++
	e.Off = uint64(d.off)
	return &e, 0
}

func (d *sliceDirStream) Seekdir(_ context.Context, off uint64) syscall.Errno {
	d.off = int(off)
	return 0
}

func (d *sliceDirStream) Releasedir(_ context.Context, _ uint32) {}
