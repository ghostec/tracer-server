package main

import (
	"bytes"
	"errors"
	"flag"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ghostec/tracer"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "0.0.0.0:8080", "http service address")

type renderer struct {
	mu sync.Mutex

	sceneFrame *tracer.Frame
	guiFrame   *tracer.Frame
	selected   *tracer.BVHNode
	hovered    *tracer.BVHNode
	scene      tracer.Hitter
	camera     tracer.Camera
	stop       chan bool
	frameId    uint64
}

func newFrame() *tracer.Frame {
	imageWidth := 500
	imageHeight := int(float64(imageWidth) / (16.0 / 9.0))
	return tracer.NewFrame(imageWidth, imageHeight, true)
}

func newRenderer() *renderer {
	return &renderer{
		sceneFrame: newFrame(),
		guiFrame:   newFrame(),
		stop:       make(chan bool, 1),
	}
}

func (r *renderer) loadScene() error {
	l := tracer.HitterList{
		tracer.Sphere{Center: tracer.Point3{0, -100.5, -1}, Radius: 100, Material: tracer.Lambertian{Albedo: tracer.Color{0.8, 0.8, 0}}},
		tracer.Sphere{Center: tracer.Point3{0, 0, -1}, Radius: 0.5, Material: tracer.Lambertian{Albedo: tracer.Color{0.1, 0.2, 0.5}}},
		tracer.Sphere{Center: tracer.Point3{-1, 0, -1}, Radius: 0.5, Material: tracer.Dielectric{RefractiveIndex: 1.5}},
		tracer.Sphere{Center: tracer.Point3{-1, 0, -1}, Radius: -0.48, Material: tracer.Dielectric{RefractiveIndex: 1.5}},
		tracer.Sphere{Center: tracer.Point3{1, 0, -1}, Radius: 0.5, Material: tracer.Metal{Albedo: tracer.Color{0.8, 0.6, 0.2}}},
	}

	bvh, err := tracer.NewBVHNode(l)
	if err != nil {
		return err
	}

	cam := tracer.Camera{
		AspectRatio: 16.0 / 9.0,
		VFoV:        90,
		LookFrom:    tracer.Point3{-0, 2, 1},
		LookAt:      tracer.Point3{0, 0, -1},
		VUp:         tracer.Vec3{0, 1, 0},
	}

	r.scene = bvh
	r.camera = cam

	return nil
}

func (r *renderer) render() {
	r.mu.Lock()
	frameId := r.frameId
	r.mu.Unlock()

	frame := newFrame()

	tracer.Render(tracer.RenderSettings{
		Frame:           frame,
		Camera:          r.camera,
		Hitter:          r.scene,
		RayColorFunc:    tracer.RayColor,
		AggColorFunc:    tracer.AvgSamples,
		SamplesPerPixel: 1,
		MaxDepth:        50,
	}, r.stop)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.frameId == frameId {
		r.sceneFrame.Avg(frame)
	}
}

func (r *renderer) renderGUI() {
	r.mu.Lock()
	frameId := r.frameId
	r.mu.Unlock()

	guiFrame := newFrame()

	if r.hovered != nil {
		hoveredBVH, err := tracer.NewBVHNode(tracer.HitterList{r.hovered.Left})
		if err != nil {
			panic(errors.New("placeholder"))
		}

		edgesFrame := tracer.NewFrame(r.sceneFrame.Width(), r.sceneFrame.Height(), true)
		tracer.Render(tracer.RenderSettings{
			Frame:           edgesFrame,
			Camera:          r.camera,
			Hitter:          hoveredBVH,
			RayColorFunc:    tracer.RayBVHID,
			AggColorFunc:    tracer.EdgeSamples,
			SamplesPerPixel: 1,
		}, r.stop)
		edgesFrame = tracer.ToEdgesFrame(edgesFrame, tracer.Color{255, 255, 0})
		guiFrame.Blend(edgesFrame, 1.0, 1.0)
	}

	if r.selected != nil {
		selectedBVH, err := tracer.NewBVHNode(tracer.HitterList{r.selected.Left})
		if err != nil {
			panic(errors.New("placeholder"))
		}

		edgesFrame := tracer.NewFrame(r.sceneFrame.Width(), r.sceneFrame.Height(), true)
		tracer.Render(tracer.RenderSettings{
			Frame:           edgesFrame,
			Camera:          r.camera,
			Hitter:          selectedBVH,
			RayColorFunc:    tracer.RayBVHID,
			AggColorFunc:    tracer.EdgeSamples,
			SamplesPerPixel: 1,
		}, r.stop)
		edgesFrame = tracer.ToEdgesFrame(edgesFrame, tracer.Color{255, 0, 0})
		guiFrame.Blend(edgesFrame, 1.0, 1.0)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.frameId == frameId {
		r.guiFrame = guiFrame
	}
}

func (r *renderer) mousemove(x, y int) {
	hr := r.scene.Hit(r.camera.GetRay(tracer.CameraCoordinatesFromPixel(y, x, r.sceneFrame.Width(), r.sceneFrame.Height())))

	r.mu.Lock()
	switch hr.Hit {
	case true:
		r.hovered = &hr.BVHNode
	case false:
		r.hovered = nil
	}
	r.mu.Unlock()

	r.renderGUI()
}

func (r *renderer) mouseclick(x, y int) {
	hr := r.scene.Hit(r.camera.GetRay(tracer.CameraCoordinatesFromPixel(y, x, r.sceneFrame.Width(), r.sceneFrame.Height())))

	r.mu.Lock()
	switch hr.Hit {
	case true:
		r.selected = &hr.BVHNode
	case false:
		r.selected = nil
	}
	r.mu.Unlock()

	r.renderGUI()
}

func (r *renderer) reset() {
	r.mu.Lock()
	close(r.stop)
	r.stop = make(chan bool, 1)
	r.sceneFrame = newFrame()
	r.guiFrame = newFrame()
	r.frameId += 1
	r.mu.Unlock()
}

func (r *renderer) Encode(w io.Writer) error {
	frame := newFrame()

	r.mu.Lock()
	frame.Blend(r.guiFrame, 1.0, 1.0)
	frame.Blend(r.sceneFrame, 1.0, 1.0)
	r.mu.Unlock()

	return png.Encode(w, tracer.NewPPM(frame))
}

var rendererObj = newRenderer()

func main() {
	flag.Parse()
	tracer.DefaultRenderer.Start()
	rendererObj.loadScene()
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
	c.EnableWriteCompression(true)

	go func() {
		for {
			start := time.Now()
			buf := bytes.NewBuffer(nil)
			if err := rendererObj.Encode(buf); err != nil {
				panic(err)
			}
			if err := c.WriteMessage(websocket.BinaryMessage, buf.Bytes()); err != nil {
				panic(err)
			}
			elapsed := time.Now().Sub(start)
			toSleep := math.Max(0.0, float64(200-elapsed.Milliseconds()))
			time.Sleep(time.Duration(toSleep) * time.Millisecond)
		}
	}()

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		// log.Printf("recv: %s", message)
		msg := string(message)
		switch {
		case msg == "1":
			rendererObj.camera.LookFrom[2] -= 0.5
		case msg == "2":
			rendererObj.camera.LookFrom[2] += 0.5
		case msg == "3":
			rendererObj.camera.LookFrom[0] -= 0.5
		case msg == "4":
			rendererObj.camera.LookFrom[0] += 0.5
		case strings.HasPrefix(msg, "mousemove") || strings.HasPrefix(msg, "mouseclick"):
			parts := strings.Split(msg, " ")
			if len(parts) != 3 {
				continue
			}
			x, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			y, err := strconv.Atoi(parts[2])
			if err != nil {
				continue
			}

			switch parts[0] {
			case "mousemove":
				rendererObj.mousemove(x, y)
			case "mouseclick":
				rendererObj.mouseclick(x, y)
			}
			fallthrough
		default:
			continue
		}
		rendererObj.reset()
	}
}

func frame(w http.ResponseWriter, r *http.Request) {
	if err := rendererObj.Encode(w); err != nil {
		log.Println("encode:", err)
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	homeTemplate.Execute(w, "ws://"+r.Host+"/ws")
}

var homeTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
</head>
<body>
	<script>  
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
			const blob = new Blob([evt.data], {type: 'image/png'});
			const el = document.getElementById("image");
			el.src = URL.createObjectURL(blob);    
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
		
		// setInterval(refreshImage, 1000);

		function _onMouseMove(event) {
			const { offsetX, offsetY } = event;
			ws.send("mousemove " + offsetX + " " + offsetY);
		}

		function debounce(func, wait, immediate) {
			var timeout;

			return function executedFunction() {
				var context = this;
				var args = arguments;
					
				var later = function() {
					timeout = null;
					if (!immediate) func.apply(context, args);
				};

				var callNow = immediate && !timeout;
			
				clearTimeout(timeout);

				timeout = setTimeout(later, wait);
			
				if (callNow) func.apply(context, args);
			};
		};

		function throttle(func, delay) {
			let timerId;

			return function executedFunction() {
				const context = this;
				const args = arguments;

				// If setTimeout is already scheduled, no need to do anything
				if (timerId) {
					return
				}

					func.apply(context, args);

				// Schedule a setTimeout after delay seconds
				timerId  =  setTimeout(function () { timerId = undefined; }, delay)
			}
		}

		const	onMouseMove = throttle(_onMouseMove, 1000)

		function onClick(event) {
		  const rect = event.target.getBoundingClientRect()
			const x = event.clientX - rect.left
			const y = event.clientY - rect.top
			ws.send("mouseclick " + x + " " + y);
		}
	</script>
	<img id="image" onclick="onClick(event)" />
</body>
</html>
`))
