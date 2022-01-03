# go-gallery

Generate a photography portfolio website for your photos. A demo is available [here](https://photos.creekorful.org).

## Usage

```
$ ./go-gallery -photos-dir ~/Photos -output-dir /var/www/photos.example.org -config-file config.yaml
```

Where `-photos-dir` is the directory where the images are stored (*.jpg, *.jpeg), `-output-dir` is the directory where
the static website will be copied and `-config-file` is the path to the configuration file.

### The configuration file

```yaml
title: "go-gallery"
url: https://photos.example.org
#cover_url: https://example.org/cover.png <- override the default cover
bg_color: "#1b1c1d"
font_color: "white"
border_size: "3px"
thumbnail_max_size: 760
month_separator: true
enable_albums: true
``` 

Once the website is generated you can upload it using lftp, rsync, etc.

The website may eventually be hosted on [Netlify](https://www.netlify.com/)

## How to hack it

In order to prevent embedded code copies, [GLightbox](https://github.com/biati-digital/glightbox) is not provided in
this repository. Therefore, if you want to hack it locally, you must first vendorize the GLightbox dependency. This can
be done using the provided Makefile.

```
$ make vendor
```
