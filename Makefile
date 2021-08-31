vendor:
ifeq (,$(wildcard ./res/vendor/glightbox.min.*))
	curl https://codeload.github.com/biati-digital/glightbox/tar.gz/refs/tags/3.1.0 -o glightbox-3.1.0.tar.gz
	tar -xzf glightbox-3.1.0.tar.gz glightbox-3.1.0
	mv glightbox-3.1.0/dist/css/glightbox.min.css res/vendor/glightbox.min.css
	mv glightbox-3.1.0/dist/js/glightbox.min.js res/vendor/glightbox.min.js
	rm -rf glightbox-3.1.0 && rm -f glightbox-3.1.0.tar.gz
endif

build: vendor
	go build -o go-gallery gallery.go