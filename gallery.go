package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/nfnt/resize"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v2"
	"html/template"
	"image/jpeg"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	sortAsc = iota
	sortDesc

	coverFileName     = "cover.jpg"
	thumbnailsDirName = "thumbnails"

	filePerm = os.FileMode(0640)
	dirPerm  = os.FileMode(0750)
)

var (
	// the program version, exported using LDFLAGS
	version = "dev"

	configFileFlag = flag.String("c", "config.yaml", "path to the configuration file")
	parallelFlag   = flag.Int64("parallel", 4, "number of parallel workers when generating photos")

	//go:embed res/*
	resDirectory embed.FS
)

// config represent the program configuration file
type config struct {
	Title            string `yaml:"title"`
	URL              string `yaml:"url"`
	CoverURL         string `yaml:"cover_url"`
	BgColor          string `yaml:"bg_color"`
	FontColor        string `yaml:"font_color"`
	BorderSize       string `yaml:"border_size"`
	ThumbnailMaxSize uint   `yaml:"thumbnail_max_size"`
	MonthSeparator   bool   `yaml:"month_separator"`
	EnableAlbums     bool   `yaml:"enable_albums"`
	PhotosSorting    string `yaml:"photos_sorting"`
}

type albumContext struct {
	Config config
	Album  album
}

type indexContext struct {
	Config config
	Albums []album
}

type album struct {
	Name   string
	Folder string
	Photos []photo
	Cover  *photo
}

type photo struct {
	Title         string    `json:"title"`
	PhotoPath     string    `json:"photo_path"`
	ThumbnailPath string    `json:"thumbnail_path"`
	ShootingDate  time.Time `json:"shooting_date,omitempty"`
	PhotoChecksum string    `json:"photo_checksum"`
}

type albumIndex struct {
	Photos []photo `json:"photos"`
	Cover  *photo  `json:"cover,omitempty"`
}

func main() {
	flag.Parse()

	log.Printf("running go-gallery %s", version)

	// Validate parameters
	if flag.NArg() == 0 {
		log.Fatalf("correct usage: go-gallery -c [config.yaml] <photos-dir>")
	}

	photosDir := flag.Arg(0)

	// Make sure photosDir exists
	_, err := os.Stat(photosDir)
	if os.IsNotExist(err) {
		log.Fatalf("directory %s does not exist", photosDir)
	}

	// Read the configuration
	config, err := readConfig(*configFileFlag)
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Generate the album(s)
	if config.EnableAlbums {
		var albums []album
		if err := filepath.Walk(photosDir, func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() && path != photosDir && info.Name() != thumbnailsDirName {
				album, err := generateAlbum(path, info.Name(), config)
				if err != nil {
					log.Fatalf("error while generating album: %s", err)
				}

				// Make sure there's photos in the directory
				if len(album.Photos) > 0 {
					albums = append(albums, album)
				}
			}

			return nil
		}); err != nil {
			log.Fatalf("error while generating album: %s", err)
		}

		// Generate the root index.html, showing the albums
		if err := executeTemplate(indexContext{Config: config, Albums: albums}, photosDir, "index.html.tmpl", "index.html"); err != nil {
			log.Fatalf("error while generating index: %s", err)
		}
		// Generate the root index.css
		if err := executeTemplate(indexContext{Config: config, Albums: albums}, photosDir, "index.css.tmpl", "index.css"); err != nil {
			log.Fatalf("error while generating index: %s", err)
		}
	} else {
		if _, err := generateAlbum(photosDir, config.Title, config); err != nil {
			log.Fatalf("error while generating album: %s", err)
		}
	}

	// Copy the third party files
	files, err := resDirectory.ReadDir(filepath.Join("res", "vendor"))
	if err != nil {
		log.Fatalf("error while processing res/vendor: %s", err)
	}
	for _, file := range files {
		srcPath := filepath.Join("vendor", file.Name())
		destPath := filepath.Join(photosDir, file.Name())

		if err := copyResFile(srcPath, destPath); err != nil {
			log.Fatalf("error while copying 3rd party file %s: %s", srcPath, err)
		}
	}

	// Copy the favicon
	if err := copyResFile(filepath.Join("favicon.png"), filepath.Join(photosDir, "favicon.png")); err != nil {
		log.Fatalf("error while copying favicon: %s", err)
	}

	log.Printf("successfully generated!")
}

// readConfig read config from given path
func readConfig(path string) (config, error) {
	f, err := os.Open(path)
	if err != nil {
		return config{}, err
	}
	defer f.Close()

	var c config
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		return config{}, err
	}

	return c, nil
}

// executeTemplate execute template identified by given name, with given context and write to given fileName in given directory
func executeTemplate(ctx interface{}, outputDirectory, templateName, fileName string) error {
	t, err := template.
		New(templateName).
		Funcs(map[string]interface{}{
			"isSameMonth": func(photos []photo, idx int) bool {
				// First photo
				if idx-1 < 0 {
					return false
				}

				left := photos[idx-1]
				right := photos[idx]

				leftShootingDate := left.ShootingDate
				rightShootingDate := right.ShootingDate

				return leftShootingDate.Year() == rightShootingDate.Year() &&
					leftShootingDate.Month() == rightShootingDate.Month()
			},
			"getAlbumCover": func(album album) string {
				if album.Cover != nil {
					return fmt.Sprintf("%s/%s", album.Folder, album.Cover.ThumbnailPath)
				}

				return fmt.Sprintf("%s/%s", album.Folder, album.Photos[0].ThumbnailPath)
			},
		}).
		ParseFS(resDirectory, filepath.Join("res", templateName))
	if err != nil {
		return err
	}

	dstPath := filepath.Join(outputDirectory, fileName)
	f, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, templateName, ctx); err != nil {
		return err
	}

	return nil
}

func generateAlbum(directory, name string, config config) (album, error) {
	// Read the previous index
	previousIndex := albumIndex{}
	b, err := ioutil.ReadFile(filepath.Join(directory, "index.json"))
	if err == nil {
		if err := json.Unmarshal(b, &previousIndex); err != nil {
			return album{}, fmt.Errorf("error while reading index.json: %s", err)
		}
	} else if !os.IsNotExist(err) {
		return album{}, fmt.Errorf("error while reading index.json: %s", err)
	}

	if err := os.MkdirAll(filepath.Join(directory, thumbnailsDirName), dirPerm); err != nil {
		return album{}, err
	}

	var photos []photo

	sem := semaphore.NewWeighted(*parallelFlag)
	workers, c := errgroup.WithContext(context.Background())
	photosMutex := sync.Mutex{}

	if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		// Ignore subdirectories
		if entry.IsDir() && directory != path {
			return fs.SkipDir
		}

		if err := sem.Acquire(c, 1); err != nil {
			return err
		}

		workers.Go(func() error {
			defer sem.Release(1)

			if !isJpegFile(entry) {
				return nil
			}

			// Read the photo
			photoBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			p := photo{}

			// Generate the photo if not already done
			if !isPhotoGenerated(photoBytes, entry.Name(), previousIndex) {
				log.Printf("[processing]\t %s", entry.Name())

				// Generate thumbnail
				photoImg, err := jpeg.Decode(bytes.NewReader(photoBytes))
				if err != nil {
					return fmt.Errorf("error while processing photo %s: %s", entry.Name(), err)
				}
				thumbFile, err := os.Create(filepath.Join(directory, thumbnailsDirName, entry.Name()))
				if err != nil {
					return fmt.Errorf("error while processing photo %s: %s", entry.Name(), err)
				}

				photoImg = resize.Thumbnail(config.ThumbnailMaxSize, config.ThumbnailMaxSize, photoImg, resize.MitchellNetravali)
				if err := jpeg.Encode(thumbFile, photoImg, nil); err != nil {
					return fmt.Errorf("error while processing photo %s: %s", entry.Name(), err)
				}

				p = photo{
					Title:         entry.Name(),
					PhotoPath:     filepath.Join(entry.Name()),
					ThumbnailPath: filepath.Join(thumbnailsDirName, entry.Name()),
				}

				// Generate the MD5 of the photos to check for changes on later execution
				hash := md5.Sum(photoBytes)
				p.PhotoChecksum = hex.EncodeToString(hash[:])

				// Try to parse photo EXIF data to get the shooting date
				if x, err := exif.Decode(bytes.NewReader(photoBytes)); err == nil {
					if tag, err := x.Get(exif.DateTimeOriginal); err == nil {
						if dateTimeStr, err := tag.StringVal(); err == nil {
							if dateTime, err := time.Parse("2006:01:02 15:04:05", dateTimeStr); err == nil {
								p.ShootingDate = dateTime
							}
						}
					}
				}
			} else {
				// use already processed photo
				log.Printf("[skipping]\t %s", entry.Name())

				for _, previousPhoto := range previousIndex.Photos {
					if previousPhoto.Title == entry.Name() {
						p = previousPhoto
						break
					}
				}
			}

			photosMutex.Lock()
			photos = append(photos, p)
			photosMutex.Unlock()

			return nil
		})

		return nil
	}); err != nil {
		return album{}, err
	}

	if err := workers.Wait(); err != nil {
		return album{}, err
	}

	sortOrder := sortDesc
	if config.PhotosSorting == "asc" {
		sortOrder = sortAsc
	}

	// sort the photos by shooting date if available
	// otherwise fallback to filename
	sort.SliceStable(photos, func(left, right int) bool {
		leftDateTime := time.Time{}
		if val := photos[left].ShootingDate; !val.IsZero() {
			leftDateTime = val
		}

		rightDateTime := time.Time{}
		if val := photos[right].ShootingDate; !val.IsZero() {
			rightDateTime = val
		}

		if !leftDateTime.IsZero() && !rightDateTime.IsZero() {
			if sortOrder == sortAsc {
				return leftDateTime.Before(rightDateTime)
			}

			return leftDateTime.After(rightDateTime)
		}

		// otherwise, fallback to filename comparison
		if sortOrder == sortAsc {
			return photos[left].Title < photos[right].Title
		}

		return photos[left].Title > photos[right].Title
	})

	// Remove removed photos
	for _, previousPhoto := range previousIndex.Photos {
		found := false
		for _, photo := range photos {
			if photo.Title == previousPhoto.Title {
				found = true
				break
			}
		}

		if !found {
			log.Printf("[deleting]\t %s", previousPhoto.Title)
			_ = os.Remove(filepath.Join(directory, previousPhoto.ThumbnailPath))
		}
	}

	a := album{
		Name: name,
		// Extract album folder from the path
		Folder: filepath.Base(directory),
		Photos: photos,
	}

	// Determinate if a cover is available
	for _, p := range photos {
		if p.Title == coverFileName {
			a.Cover = &photo{
				Title:         p.Title,
				PhotoPath:     p.PhotoPath,
				ThumbnailPath: p.ThumbnailPath,
				ShootingDate:  p.ShootingDate,
				PhotoChecksum: p.PhotoChecksum,
			}
			break
		}
	}

	ctx := albumContext{Config: config, Album: a}

	// Generate the index.json
	indexBytes, err := json.Marshal(albumIndex{Photos: photos, Cover: a.Cover})
	if err != nil {
		return album{}, fmt.Errorf("error while generating index.json: %s", err)
	}
	if err := ioutil.WriteFile(filepath.Join(directory, "index.json"), indexBytes, filePerm); err != nil {
		return album{}, fmt.Errorf("error while generating index.json: %s", err)
	}

	// Generate the index.html
	if err := executeTemplate(ctx, directory, "album.html.tmpl", "index.html"); err != nil {
		return album{}, fmt.Errorf("error while generating index.html: %s", err)
	}

	// Generate the index.css
	if err := executeTemplate(ctx, directory, "album.css.tmpl", "index.css"); err != nil {
		return album{}, fmt.Errorf("error while generating index.css: %s", err)
	}

	return a, nil
}

// isPhotoGenerated determinate if given photo has been already generated (i.e: thumbnail is up-to-date and photo is copied if needed)
func isPhotoGenerated(photoBytes []byte, photoTitle string, previousIndex albumIndex) bool {
	photoIdx := -1

	for i, current := range previousIndex.Photos {
		if current.Title == photoTitle {
			photoIdx = i
			break
		}
	}

	if photoIdx == -1 {
		return false
	}

	hash := md5.Sum(photoBytes)

	return previousIndex.Photos[photoIdx].PhotoChecksum == hex.EncodeToString(hash[:])
}

func isJpegFile(entry fs.DirEntry) bool {
	fileName := strings.ToLower(entry.Name())
	return strings.HasSuffix(fileName, ".jpg") || strings.HasSuffix(fileName, ".jpeg")
}

func copyResFile(srcPath, dstPath string) error {
	content, err := resDirectory.ReadFile(filepath.Join("res", srcPath))
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dstPath, content, filePerm)
}
