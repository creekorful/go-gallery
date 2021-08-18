package main

import (
	"bytes"
	"embed"
	_ "embed"
	"flag"
	"github.com/nfnt/resize"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/sync/errgroup"
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

var (
	// the program version, exported using LDFLAGS
	version = "dev"

	photosDirFlag  = flag.String("photos-dir", "", "")
	distDirFlag    = flag.String("output-dir", "dist", "")
	configFileFlag = flag.String("config-file", "config.yaml", "")

	//go:embed res/*
	resDirectory embed.FS

	filePerm = os.FileMode(0640)
	dirPerm  = os.FileMode(0750)
)

// the program configuration file
type config struct {
	Title            string `yaml:"title"`
	BgColor          string `yaml:"bg_color"`
	FontColor        string `yaml:"font_color"`
	Border           string `yaml:"border"`
	ThumbnailMaxSize uint   `yaml:"thumbnail_max_size"`
	ShowSeparator    bool   `yaml:"show_separator"`
}

type context struct {
	Config config
	Photos []map[string]interface{}
}

func main() {
	flag.Parse()

	log.Printf("running go-gallery %s", version)

	// Validate parameters
	if *photosDirFlag == "" {
		log.Fatalf("missing required parameter --photos-dir")
	}

	// Make sure photosDir exists
	_, err := os.Stat(*photosDirFlag)
	if os.IsNotExist(err) {
		log.Fatalf("directory %s does not exist", *photosDirFlag)
	}

	config, err := readConfig(*configFileFlag)
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Create dist folder
	if err := os.MkdirAll(*distDirFlag, dirPerm); err != nil {
		log.Fatalf("error while creating %s/ folder: %s", *distDirFlag, err)
	}

	photos, err := processPhotos(*photosDirFlag, *distDirFlag, config.ThumbnailMaxSize)
	if err != nil {
		log.Fatalf("error while processing photos: %s", err)
	}

	ctx := context{Config: config, Photos: photos}

	// Generate the index.html
	if err := executeTemplate(ctx, *distDirFlag, "index.html.tmpl"); err != nil {
		log.Fatalf("error while generating index.html: %s", err)
	}

	// Generate the index.css
	if err := executeTemplate(ctx, *distDirFlag, "index.css.tmpl"); err != nil {
		log.Fatalf("error while generating index.css: %s", err)
	}

	// Copy the third party files
	files, err := resDirectory.ReadDir(filepath.Join("res", "vendor"))
	if err != nil {
		log.Fatalf("error while processing res/vendor: %s", err)
	}

	for _, file := range files {
		srcPath := filepath.Join("vendor", file.Name())
		destPath := filepath.Join(*distDirFlag, file.Name())

		if err := copyResFile(srcPath, destPath); err != nil {
			log.Fatalf("error while copying 3rd party file %s: %s", srcPath, err)
		}
	}

	// Copy the favicon
	if err := copyResFile(filepath.Join("favicon.png"), filepath.Join(*distDirFlag, "favicon.png")); err != nil {
		log.Fatalf("error while copying favicon: %s", err)
	}

	log.Printf("successfully generated! (%d photos)", len(photos))
}

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

func executeTemplate(ctx context, distDirectory, templateName string) error {
	t, err := template.
		New(templateName).
		Funcs(map[string]interface{}{
			"samePeriod": func(photos []map[string]interface{}, idx int) bool {
				left := photos[idx]

				var right map[string]interface{}
				if idx+1 < len(photos) {
					right = photos[idx+1]
				} else {
					// The end
					return false
				}

				leftShootingDate := left["ShootingDate"].(time.Time)
				rightShootingDate := right["ShootingDate"].(time.Time)

				return leftShootingDate.Year() == rightShootingDate.Year() &&
					leftShootingDate.Month() == rightShootingDate.Month()
			},
		}).
		ParseFS(resDirectory, filepath.Join("res", templateName))
	if err != nil {
		return err
	}

	dstPath := filepath.Join(distDirectory, strings.TrimSuffix(templateName, ".tmpl"))
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

func processPhotos(photosDir, distDirectory string, thumbnailMaxSize uint) ([]map[string]interface{}, error) {
	if err := os.MkdirAll(filepath.Join(distDirectory, "photos", "thumbnails"), dirPerm); err != nil {
		return nil, err
	}

	var photos []map[string]interface{}

	workers := errgroup.Group{}
	photosMutex := sync.Mutex{}

	if err := filepath.Walk(photosDir, func(path string, info fs.FileInfo, err error) error {
		workers.Go(func() error {
			if !isJpegFile(info) {
				return nil
			}

			// Read the photo
			photoBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			// Determinate if the photo is not already processed
			photoDstPath := filepath.Join(distDirectory, "photos", info.Name())
			if !isPhotoProcessed(photoBytes, photoDstPath) {
				log.Printf("processing %s", info.Name())

				thumbnailDstPath := filepath.Join(distDirectory, "photos", "thumbnails", info.Name())

				// Generate thumbnail
				photo, err := jpeg.Decode(bytes.NewReader(photoBytes))
				if err != nil {
					return err
				}
				thumbFile, err := os.Create(thumbnailDstPath)
				if err != nil {
					return err
				}

				photo = resize.Thumbnail(thumbnailMaxSize, thumbnailMaxSize, photo, resize.MitchellNetravali)
				if err := jpeg.Encode(thumbFile, photo, nil); err != nil {
					return err
				}

				// Copy the photo
				if err := ioutil.WriteFile(photoDstPath, photoBytes, filePerm); err != nil {
					return err
				}
			} else {
				log.Printf("skipping existing photo %s", info.Name())
			}

			photo := map[string]interface{}{
				"Title":         info.Name(),
				"PhotoPath":     filepath.Join("photos", info.Name()),
				"ThumbnailPath": filepath.Join("photos", "thumbnails", info.Name()),
			}

			// Try to parse photo EXIF data to get the shooting date
			if x, err := exif.Decode(bytes.NewReader(photoBytes)); err == nil {
				if tag, err := x.Get(exif.DateTimeOriginal); err == nil {
					if dateTimeStr, err := tag.StringVal(); err == nil {
						if dateTime, err := time.Parse("2006:01:02 15:04:05", dateTimeStr); err == nil {
							photo["ShootingDate"] = dateTime
						}
					}
				}
			}

			photosMutex.Lock()
			photos = append(photos, photo)
			photosMutex.Unlock()

			return nil
		})

		return workers.Wait()
	}); err != nil {
		return nil, err
	}

	// sort the photos by shooting date if available
	// otherwise fallback to filename
	sort.SliceStable(photos, func(left, right int) bool {
		leftDateTime := time.Time{}
		if val, exists := photos[left]["ShootingDate"]; exists {
			leftDateTime = val.(time.Time)
		}

		rightDateTime := time.Time{}
		if val, exists := photos[right]["ShootingDate"]; exists {
			rightDateTime = val.(time.Time)
		}

		if !leftDateTime.IsZero() && !rightDateTime.IsZero() {
			return leftDateTime.After(rightDateTime)
		}

		// otherwise, fallback to filename comparison
		return photos[left]["Title"].(string) > photos[right]["Title"].(string)
	})

	return photos, nil
}

func isPhotoProcessed(photoBytes []byte, destPath string) bool {
	_, err := os.Stat(destPath)
	if os.IsNotExist(err) {
		return false
	}

	// Photo already exists, read it and compare byte-by-byte to determinate if file has changed
	destPhotoBytes, err := os.ReadFile(destPath)
	if err != nil {
		// todo
	}

	return bytes.Equal(photoBytes, destPhotoBytes)
}

func isJpegFile(file fs.FileInfo) bool {
	return !file.IsDir() && (strings.HasSuffix(file.Name(), ".jpg") || strings.HasSuffix(file.Name(), ".jpeg"))
}

func copyResFile(srcPath, dstPath string) error {
	content, err := resDirectory.ReadFile(filepath.Join("res", srcPath))
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dstPath, content, filePerm)
}
