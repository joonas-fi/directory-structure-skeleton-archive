package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/function61/gokit/app/cli"
	"github.com/function61/gokit/app/dynversion"
	"github.com/function61/gokit/os/osutil"
	"github.com/spf13/cobra"
)

func main() {
	osutil.ExitIfError((&cobra.Command{
		Use:     os.Args[0] + " [dir]",
		Short:   "Creates skeleton .zip that represent how a directory hierarchy looks like, without storing file contents",
		Version: dynversion.Version,
		Args:    cobra.MinimumNArgs(1),
		Run: cli.Runner(func(ctx context.Context, args []string, _ *log.Logger) error {
			return logic(ctx, args)
		}),
	}).Execute())
}

func logic(ctx context.Context, dirs []string) error {
	return osutil.WriteFileAtomic("out.zip", func(file io.Writer) error {
		zipWriter := zip.NewWriter(file)

		// no need to change default compression level. here's results from Video + Pictures collection of 163 GB:
		//
		// DefaultCompression = 164M
		// BestCompression = 164M
		// BestSpeed = 204M
		// HuffmanOnly = huge file size

		for _, dir := range dirs {
			if err := zipOneDir(ctx, dir, zipWriter); err != nil {
				return err
			}
		}

		if err := zipWriter.SetComment("written by directory-structure-skeleton-archive"); err != nil {
			return err
		}

		readme, err := zipWriter.CreateHeader(&zip.FileHeader{
			Name:     "README-this-archive-is-special.txt",
			Modified: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		if _, err := readme.Write([]byte("This archive contains only metadata about the files. The file contents are filled with null.")); err != nil {
			return err
		}

		return zipWriter.Close()
	})
}

func zipOneDir(ctx context.Context, dir string, zipWriter *zip.Writer) error {
	if err := filepath.WalkDir(dir, func(path string, dirEntry fs.DirEntry, err error) error {
		withErr := func(err error) error {
			return fmt.Errorf("%s: %w", path, err)
		}

		if err != nil {
			return withErr(err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// continue
		}

		fmt.Println(path)

		fileInfo, err := dirEntry.Info()
		if err != nil {
			return withErr(err)
		}

		if fileInfo.IsDir() {
			return nil
		}

		zipInfo, err := zip.FileInfoHeader(fileInfo)
		if err != nil {
			return withErr(err)
		}

		// > If compression is desired, callers should set the FileHeader.Method field; it is unset by default.
		zipInfo.Method = zip.Deflate

		// > Because fs.FileInfo's Name method returns only the base name of the file it describes, it may be
		// > necessary to modify the Name field of the returned header to provide the full path name of the file.
		zipInfo.Name = func() string {
			if fileInfo.IsDir() {
				// > To write an empty directory you just need to call Create with the directory path with a trailing path separator.
				// https://stackoverflow.com/a/70482137
				return path + "/"
			} else {
				return path
			}
		}()

		objectInZip, err := zipWriter.CreateHeader(zipInfo)
		if err != nil {
			return withErr(err)
		}

		if !fileInfo.IsDir() { // only files have content
			fileZeroContent := io.LimitReader(readAllZeroes, fileInfo.Size())

			// adding buffered writer (with 1 MB buffer size) does not improve compression ratio.
			// this implies there's already optimal buffering going on.
			if _, err := io.Copy(objectInZip, fileZeroContent); err != nil {
				return withErr(err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("zipOneDir: %w", err)
	}

	return nil
}

var (
	// can share this instance
	readAllZeroes = &nullReader{}
)

type nullReader struct{}

var _ io.Reader = (*nullReader)(nil)

func (n *nullReader) Read(buf []byte) (int, error) {
	for i := range buf {
		buf[i] = 0x00
	}

	return len(buf), nil
}
