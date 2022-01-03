# go-gallery

Generate a photography portfolio website for your photos. A demo is available [here](https://photos.creekorful.org).

## Usage

```
$ ./go-gallery -c config.yaml -parallel 8 ~/Photos
```

Where `~Photos` is the directory where the images are stored (*.jpg, *.jpeg).

The software will generate a bunch of .html and .css file to turn your directory as a static website, that you 
can upload to Netlify, S3, or an FTP server afterwards.

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

## How to hack it

In order to prevent embedded code copies, [GLightbox](https://github.com/biati-digital/glightbox) is not provided in
this repository. Therefore, if you want to hack it locally, you must first vendorize the GLightbox dependency. This can
be done using the provided Makefile.

```
$ make vendor
```
