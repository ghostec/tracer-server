package main

import (
	"flag"
	"image/png"
	"io"
	"log"
	"net/http"
	"sync"
	"text/template"

	"github.com/ghostec/tracer"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "0.0.0.0:8080", "http service address")

var lookFrom = tracer.Point3{-0, 2, 1}

type renderer struct {
	frame   *tracer.Frame
	stop    chan bool
	mu      sync.Mutex
	frameId uint64
}

func newFrame() *tracer.Frame {
	imageWidth := 1000
	imageHeight := int(float64(imageWidth) / (16.0 / 9.0))
	return tracer.NewFrame(imageWidth, imageHeight)
}

func newRenderer() *renderer {
	return &renderer{
		frame: newFrame(),
		stop:  make(chan bool, 1),
	}
}

func (r *renderer) render() {
	r.mu.Lock()
	frameId := r.frameId
	r.mu.Unlock()
	frame := newFrame()
	render(frame, lookFrom, r.stop)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.frameId == frameId {
		r.frame.Avg(frame)
	}
}

func (r *renderer) reset() {
	r.mu.Lock()
	close(r.stop)
	r.stop = make(chan bool, 1)
	r.frame = newFrame()
	r.frameId += 1
	r.mu.Unlock()
}

func (r *renderer) Encode(w io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return png.Encode(w, tracer.NewPPM(r.frame))
}

var rendererObj = newRenderer()

func main() {
	flag.Parse()
	http.HandleFunc("/ws", ws)
	http.HandleFunc("/frame.png", frame)
	http.HandleFunc("/", home)
	go func() {
		for {
			rendererObj.render()
		}
	}()
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func ws(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		// log.Printf("recv: %s", message)
		switch string(message) {
		case "1":
			lookFrom[2] -= 0.5
		case "2":
			lookFrom[2] += 0.5
		case "3":
			lookFrom[0] -= 0.5
		case "4":
			lookFrom[0] += 0.5
		}
		rendererObj.reset()
	}
}

func frame(w http.ResponseWriter, r *http.Request) {
	if err := rendererObj.Encode(w); err != nil {
		log.Println("encode:", err)
	}
}

func render(frame *tracer.Frame, lookFrom tracer.Point3, stop <-chan bool) {
	var l tracer.HitterList
	{
		l = tracer.HitterList{
			tracer.Sphere{Center: tracer.Point3{0, -100.5, -1}, Radius: 100, Material: tracer.Lambertian{Albedo: tracer.Color{0.8, 0.8, 0}}},
			tracer.Sphere{Center: tracer.Point3{0, 0, -1}, Radius: 0.5, Material: tracer.Lambertian{Albedo: tracer.Color{0.1, 0.2, 0.5}}},
			tracer.Sphere{Center: tracer.Point3{-1, 0, -1}, Radius: 0.5, Material: tracer.Dielectric{RefractiveIndex: 1.5}},
			tracer.Sphere{Center: tracer.Point3{-1, 0, -1}, Radius: -0.48, Material: tracer.Dielectric{RefractiveIndex: 1.5}},
			tracer.Sphere{Center: tracer.Point3{1, 0, -1}, Radius: 0.5, Material: tracer.Metal{Albedo: tracer.Color{0.8, 0.6, 0.2}}},
		}
	}

	cam := tracer.Camera{
		AspectRatio: 16.0 / 9.0,
		VFoV:        90,
		LookFrom:    lookFrom,
		LookAt:      tracer.Point3{0, 0, -1},
		VUp:         tracer.Vec3{0, 1, 0},
	}

	tracer.Render(frame, cam, l, 1, 20, stop)
}

func home(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/ws")
}

var homeTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script>  
window.addEventListener("load", function(evt) {
    var ws;
		ws = new WebSocket("{{.}}");
		ws.onopen = function(evt) {
				document.onkeypress = function (e) {
						e = e || window.event;
						switch (String.fromCharCode(e.keyCode)) {
								case "w":
										ws.send(1);
										break;
								case "s":
										ws.send(2);
										break;
								case "a":
										ws.send(3);
										break;
								case "d":
										ws.send(4);
										break;
						}
				};
		}
		ws.onclose = function(evt) {
				ws = null;
		}
		ws.onmessage = function(evt) {
				const canvas = document.getElementById('canvas');
				const ctx = canvas.getContext('2d');

				const lines = evt.data.split("\n");
				const [width, height] = lines[1].split(" ").map(val => parseInt(val));

				let myImageData = ctx.createImageData(width, height);
				myImageData.data = new Uint8ClampedArray(width * height * 4)
				let counter = 0
				lines.slice(3).join(" ").split(" ").map(val => parseInt(val)).forEach((val, i) => {
					myImageData.data[counter] = val
					counter += 1
					if ((i+1) % 3 === 0) {
						myImageData.data[counter] = 255
						counter += 1
					}
				})
				ctx.putImageData(myImageData, 0, 0)
		}
		ws.onerror = function(evt) {
				console.log("ERROR: " + evt.data);
		}

		function refreshImage() {    
				const timestamp = new Date().getTime();  
				const el = document.getElementById("image");
				const queryString = "?t=" + timestamp;
				el.src = "frame.png" + queryString;    
		}
		
		setInterval(refreshImage, 500);
});
</script>
</head>
<body>
	<img id="image" />
</body>
</html>
`))
