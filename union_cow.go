package afero

import (
	"os"
	"syscall"
	"time"
)

// The CopyOnWriteUnionFs is a union filesystem: a read only base file system with
// a possibly writeable layer on top. Changes to the file system will only
// be made in the overlay: Changing an existing file in the base layer which
// is not present in the overlay will copy the file to the overlay ("changing"
// includes also calls to e.g. Chtimes() and Chmod()).
// The overlay is currently limited to MemMapFs:
//  - missing MkdirAll() calls in the code below, MemMapFs creates them
//    implicitly (or better: records the full path and afero.Readdir()
//    can handle this).
//
// Reading directories is currently only supported via Open(), not OpenFile().
type CopyOnWriteUnionFs struct {
	base  Fs
	layer Fs
}

func (u *CopyOnWriteUnionFs) isBaseFile(name string) (bool, error) {
	if _, err := u.layer.Stat(name); err == nil {
		return false, nil
	}
	_, err := u.base.Stat(name)
	return true, err
}

func (u *CopyOnWriteUnionFs) copyToLayer(name string) error {
	return copyToLayer(u.base, u.layer, name)
}

func (u *CopyOnWriteUnionFs) Chtimes(name string, atime, mtime time.Time) error {
	b, err := u.isBaseFile(name)
	if err != nil {
		return err
	}
	if b {
		if err := u.copyToLayer(name); err != nil {
			return err
		}
	}
	return u.layer.Chtimes(name, atime, mtime)
}

func (u *CopyOnWriteUnionFs) Chmod(name string, mode os.FileMode) error {
	b, err := u.isBaseFile(name)
	if err != nil {
		return err
	}
	if b {
		if err := u.copyToLayer(name); err != nil {
			return err
		}
	}
	return u.layer.Chmod(name, mode)
}

func (u *CopyOnWriteUnionFs) Stat(name string) (os.FileInfo, error) {
	fi, err := u.layer.Stat(name)
	switch err {
	case nil:
		return fi, nil
	case syscall.ENOENT:
		return u.base.Stat(name)
	default:
		return nil, err
	}
}

// Renaming files present only in the base layer is not permitted
func (u *CopyOnWriteUnionFs) Rename(oldname, newname string) error {
	b, err := u.isBaseFile(oldname)
	if err != nil {
		return err
	}
	if b {
		return syscall.EPERM
	}
	return u.layer.Rename(oldname, newname)
}

// Removing files present only in the base layer is not permitted. If
// a file is present in the base layer and the overlay, only the overlay
// will be removed.
func (u *CopyOnWriteUnionFs) Remove(name string) error {
	err := u.layer.Remove(name)
	switch err {
	case syscall.ENOENT:
		_, err = u.base.Stat(name)
		if err == nil {
			return syscall.EPERM
		}
		return syscall.ENOENT
	default:
		return err
	}
}

func (u *CopyOnWriteUnionFs) RemoveAll(name string) error {
	err := u.layer.RemoveAll(name)
	switch err {
	case syscall.ENOENT:
		_, err = u.base.Stat(name)
		if err == nil {
			return syscall.EPERM
		}
		return syscall.ENOENT
	default:
		return err
	}
}

func (u *CopyOnWriteUnionFs) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	b, err := u.isBaseFile(name)
	if err != nil {
		return nil, err
	}

	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		if b {
			if err = u.copyToLayer(name); err != nil {
				return nil, err
			}
		}
		return u.layer.OpenFile(name, flag, perm)
	}
	if b {
		return u.base.OpenFile(name, flag, perm)
	}
	return u.layer.OpenFile(name, flag, perm)
}

func (u *CopyOnWriteUnionFs) Open(name string) (File, error) {
	b, err := u.isBaseFile(name)
	if err != nil {
		return nil, err
	}
	if b {
		return u.base.Open(name)
	}

	dir, err := IsDir(u.layer, name)
	if err != nil {
		return nil, err
	}
	if !dir {
		return u.layer.Open(name)
	}

	bfile, _ := u.base.Open(name)
	lfile, err := u.layer.Open(name)
	if err != nil && bfile == nil {
		return nil, err
	}
	return &UnionFile{base: bfile, layer: lfile}, nil
}

func (u *CopyOnWriteUnionFs) Mkdir(name string, perm os.FileMode) error {
	dir, err := IsDir(u.base, name)
	if err != nil {
		return u.layer.MkdirAll(name, perm)
	}
	if dir {
		return syscall.EEXIST
	}
	return u.layer.MkdirAll(name, perm)
}

func (u *CopyOnWriteUnionFs) Name() string {
	return "CopyOnWriteUnionFs"
}

func (u *CopyOnWriteUnionFs) MkdirAll(name string, perm os.FileMode) error {
	dir, err := IsDir(u.base, name)
	if err != nil {
		return u.layer.MkdirAll(name, perm)
	}
	if dir {
		return syscall.EEXIST
	}
	return u.layer.MkdirAll(name, perm)
}

func (u *CopyOnWriteUnionFs) Create(name string) (File, error) {
	b, err := u.isBaseFile(name)
	if err == nil && b {
		if err = u.copyToLayer(name); err != nil {
			return nil, err
		}
	}
	return u.layer.Create(name)
}
