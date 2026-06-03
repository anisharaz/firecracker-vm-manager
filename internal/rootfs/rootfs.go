// Package rootfs provisions per-VM root filesystem images by copying a raw
// .img template and growing it (sparsely) to the requested size.
//
// The package only manipulates the block device file. It does NOT resize
// the filesystem inside; the guest is responsible for resize2fs (or its
// equivalent) on first boot.
package rootfs

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrSmallerThanTemplate is returned when the requested size is below the
// template image size.
var ErrSmallerThanTemplate = errors.New("rootfs: requested size is smaller than template")

// Provision copies template -> dest and grows dest to sizeMiB MiB if larger.
// dest must not already exist.
func Provision(template, dest string, sizeMiB int64) error {
	srcInfo, err := os.Stat(template)
	if err != nil {
		return fmt.Errorf("rootfs: stat template: %w", err)
	}
	desiredBytes := sizeMiB * 1024 * 1024
	if desiredBytes < srcInfo.Size() {
		return fmt.Errorf("%w: template=%dB requested=%dB", ErrSmallerThanTemplate, srcInfo.Size(), desiredBytes)
	}

	src, err := os.Open(template)
	if err != nil {
		return fmt.Errorf("rootfs: open template: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("rootfs: create dest: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(dest)
		return fmt.Errorf("rootfs: copy: %w", err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(dest)
		return fmt.Errorf("rootfs: close: %w", err)
	}

	if desiredBytes > srcInfo.Size() {
		if err := os.Truncate(dest, desiredBytes); err != nil {
			os.Remove(dest)
			return fmt.Errorf("rootfs: truncate: %w", err)
		}
	}
	return nil
}

// Cleanup removes a per-VM directory tree.
func Cleanup(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("rootfs: cleanup: %w", err)
	}
	return nil
}
