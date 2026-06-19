// Package ingest loads USDA FoodData Central CSV data into Postgres.
package ingest

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Source yields named CSV files ("food.csv", "food_nutrient.csv") from
// wherever the dataset lives: a directory, a zip, or a downloaded URL.
type Source interface {
	Open(name string) (io.ReadCloser, error)
	Name() string // recorded in ingest_runs.source
	Close() error
}

// NewSource resolves arg: an http(s) URL (downloads the zip to a temp file),
// a .zip path, or a directory of CSVs.
func NewSource(ctx context.Context, arg string) (Source, error) {
	switch {
	case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "https://"):
		return newURLSource(ctx, arg)
	case strings.HasSuffix(arg, ".zip"):
		return newZipSource(arg, arg, false)
	default:
		return dirSource{dir: arg}, nil
	}
}

type dirSource struct {
	dir string
}

func (s dirSource) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.dir, name))
}

func (s dirSource) Name() string { return s.dir }
func (s dirSource) Close() error { return nil }

type zipSource struct {
	rc      *zip.ReadCloser
	name    string
	tmpPath string // non-empty for downloaded zips; removed on Close
}

func newZipSource(zipPath, name string, temp bool) (*zipSource, error) {
	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	s := &zipSource{rc: rc, name: name}
	if temp {
		s.tmpPath = zipPath
	}
	return s, nil
}

func (s *zipSource) Open(name string) (io.ReadCloser, error) {
	for _, f := range s.rc.File {
		if path.Base(f.Name) == name { // FDC zips nest CSVs in a top-level folder
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%s not found in %s", name, s.name)
}

func (s *zipSource) Name() string { return s.name }

func (s *zipSource) Close() error {
	err := s.rc.Close()
	if s.tmpPath != "" {
		_ = os.Remove(s.tmpPath)
	}
	return err
}

func newURLSource(ctx context.Context, url string) (Source, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp("", "fdc-*.zip")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return nil, err
	}
	return newZipSource(tmp.Name(), url, true)
}
