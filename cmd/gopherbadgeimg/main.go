package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/makeworld-the-better-one/dither"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// flags for determining what to do
var (
	disableDithering bool
	outMode          string
	show             bool
	ratio            string
)

func main() {
	flag.BoolVar(&disableDithering, "disable-dithering", false, "disables dithering")
	flag.BoolVar(&show, "show", false, "paints dot-matrix-style art to the screen representing the image")
	flag.StringVar(
		&outMode,
		"outmode",
		"",
		"set the resize mode to one of: rice, bin, base64, or none",
	)
	flag.StringVar(
		&ratio,
		"ratio",
		"",
		"set the aspect ratio to predefined values including 'profile' or splash', or a custom value specified in the format of <height>x<width>.",
	)
	flag.Parse()
	if flag.NArg() != 1 {
		log.Printf("args: %v\n\n", flag.Args())
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s <input_image>:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"\nExamples:\n%s -outmode bin -ratio splash tainigo_128.png\n%s -outmode rice -ratio 128x128 -disable-dithering -show image.jpg\n",
			os.Args[0],
			os.Args[0],
		)
		os.Exit(1)
		return
	}
	infile := flag.Args()[0]
	if _, err := os.Stat(infile); err != nil {
		log.Fatalf("could not stat %v: %v", infile, err)
	}
	sourceImage, err := LoadImg(infile)
	if err != nil {
		log.Fatalf("error loading source image: %v", err)
	}
	var imgBits []byte

	var x, y int
	switch ratio {
	case "profile":
		// profile image is 128x128
		x, y = 120, 128
	case "splash":
		// splash image is 246x128
		x, y = 246, 128
	case "":
		log.Println("error: a ratio must be provided.\n")
		Usage()
		return
	default:
		x, y, err = ParseRatio(ratio)
		if err != nil {
			log.Println(err.Error())
			Usage()
			// The Usage function calls os.Exit(1) but LSPs and static analyzers often don't
			// pick up on that, so it's good practice to return from the caller anyway
			// For the sake of consistency, we return after toplevel os.Exit calls as well
			return
		}
		// must use a y value divisble by 8 as we write the bits one byte at a time
		if y%8 != 0 {
			log.Println("error: height/y value must be divisible by 8")
			os.Exit(1)
			return
		}

	}
	imgBits = ImgToBytes(x, y, sourceImage)
	switch outMode {
	case "rice":
		err = WriteToGoFile(fmt.Sprintf("%s-generated.go", ratio), ratio, imgBits)
		if err != nil {
			log.Fatalf("error writing image to file: %v", err)
		}
	case "bin":
		err = WriteToBinFile(fmt.Sprintf("%s.bin", ratio), imgBits)
		if err != nil {
			log.Fatalf("error writing image to file: %v", err)
		}
	case "base64":
		fmt.Println(EncodeToString(imgBits))
	case "none":
		// this option is useful if you want to preview the file without creating it
	default:
		log.Printf("error: invalid outmode `%s`\n\n", outMode)
		Usage()
		return
	}
	if show {
		PrintImg(x, y, imgBits)
	}
}

// EncodeToString is a friendly-named function for hooking into base64
func EncodeToString(imageBits []byte) string {
	return base64.StdEncoding.EncodeToString(imageBits)
}

// WriteToBinFile create an go:embed-able file containing the image data.
//
// Provides a .bin file that's just the raw bytes of the image.
// You can then use a go:embed directive to bake this bin file into your code at
// compile time (be nice to your editor's memory!).
// see an example of this in the main_test.go file.
func WriteToBinFile(filename string, imageBits []byte) error {
	outf, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer outf.Close()
	_, err = outf.Write(imageBits)
	return err
}

// Create a go file with the bytes hardcoded into a variable at build
func WriteToGoFile(filename, variablename string, imageBits []byte) error {
	outf, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer outf.Close()
	_, err = outf.Write(
		[]byte(
			"// Code generated by " + os.Args[0] + " DO NOT EDIT.\n\npackage main\n\nvar r" + variablename + " = []byte{",
		),
	)
	if err != nil {
		return err
	}

	for i, b := range imageBits {
		if i%32 == 0 {
			_, err = outf.Write([]byte("\n\t"))
			if err != nil {
				return err
			}
		}
		bStr := fmt.Sprintf("0x%02X, ", b)
		_, err = outf.Write([]byte(bStr))
		if err != nil {
			return err
		}
	}
	_, err = outf.Write([]byte("\n}\n"))
	return err
}

// LoadImg loads and decodes filename into image.Image pointer
func LoadImg(infile string) (*image.Image, error) {
	f, err := os.Open(infile)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return &src, nil
}

// ImgToBytes resizes an image to the requested size and converts it to a bitmap byte slice
func ImgToBytes(x, y int, inputImg *image.Image) []byte {
	// work on values not pointers
	src := *inputImg
	// create a new, rectangular image that's the size we want
	dst := image.NewRGBA(image.Rect(0, 0, x, y))
	// use NearestNeighbor algo to fit our original image into the smaller (or bigger!?) image
	draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)

	// Our e-ink display uses one bit for each pixel, on or off.
	// Therefore, we need one bit for each pixel.
	// Since we have a byte slice, and 8 bytes per bit, divide by 8
	imageBits := make([]byte, x*y/8)

	// Again, on or off, white or black are our only color options
	palette := []color.Color{
		color.Black,
		color.White,
	}

	if disableDithering {
		// don't dither image if flag is set, useful for some images which are already black and white
	} else {
		// using our palette, create a dithering struct
		// and dither our image to get some false shading.
		// read more here: https://en.wikipedia.org/wiki/Floyd%E2%80%93Steinberg_dithering
		d := dither.NewDitherer(palette)
		d.Matrix = dither.FloydSteinberg
		dithered := d.Dither(dst)
		// this nil check is necessary since the library will often write
		// the dithered image to dst, but not always. Read their docs for more info
		if dithered != nil {
			var ok bool
			dst, ok = dithered.(*image.RGBA)
			// docs claim image is guaranteed to be of this type when not nil, but it's good to check anyway
			if !ok {
				log.Fatalf("error: typeof dithered should have been `*image.RGBA` but was `%T`", dithered)
			}
		}
	}

	// loop over the x axis first, then y as screen updates LTR, top to bottom
	// (vertical axis must be inner loop) for the badge layout
	for i := 0; i < x; i++ {
		for j := 0; j < y; j++ {
			// grab dithered image point, determine if bit should be 1 or a 0
			r, g, b, _ := dst.At(i, j).RGBA()
			if r+g+b == 0 {
				// use bit shifting + integer division & modulo arithmetic to change
				// the individual bits we want to set
				imageBits[(i*y+j)/8] = imageBits[(i*y+j)/8] | (1 << uint(7-(i*y+j)%8))
			}
		}
	}
	return imageBits
}

func ParseRatio(rstr string) (int, int, error) {
	rstr = strings.ToLower(rstr)
	pixels := strings.Split(rstr, "x")
	if len(pixels) != 2 {
		return 0, 0, errors.New("invalid ratio string provided")
	}
	x, err := strconv.Atoi(pixels[0])
	if err != nil {
		return 0, 0, errors.Join(errors.New("error: could not parse x coordinate count"), err)
	}
	y, err := strconv.Atoi(pixels[0])
	if err != nil {
		return 0, 0, errors.Join(errors.New("error: could not parse y coordinate count"), err)
	}
	return x, y, nil
}

// Usage prints a proper example of usage for when the user misuses the program.
//
// Usage also calls exit(1) to terminate the program with an error code.
func Usage() {
	fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s <input_image>:\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(
		flag.CommandLine.Output(),
		"\nExamples:\n%s input.png -outmode bin -ratio profile\n%s input.jpg -outmode rice -ratio 128x128 -disable-dithering -show\n",
		os.Args[0],
		os.Args[0],
	)
	os.Exit(1)
}

// PrintImg prints an `*` for each marked bit
//
// It writes to stderr so that it doesn't conflict with the base64 output
func PrintImg(x, y int, imgBits []byte) {
	for i := 0; i < y; i++ {
		for j := 0; j < x; j++ {
			offset := j*y + i
			bit := imgBits[offset/8] & (1 << uint(7-offset%8))
			if bit != 0 {
				fmt.Fprint(os.Stderr, "*")
			} else {
				fmt.Fprint(os.Stderr, " ")
			}
		}
		fmt.Fprint(os.Stderr, "\n")
	}
}
