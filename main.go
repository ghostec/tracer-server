package main

import (
	"flag"
	"log"
	"net/http"
	"text/template"

	"github.com/ghostec/tracer"
	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", "0.0.0.0:8080", "http service address")

func main() {
	flag.Parse()
	http.HandleFunc("/ws", ws)
	http.HandleFunc("/", home)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

var lookFrom = tracer.Point3{-0, 2, 1}

func ws(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)
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
		message = []byte(render(lookFrom).PPM())
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func render(lookFrom tracer.Point3) *tracer.Frame {
	imageWidth := 1000
	imageHeight := int(float64(imageWidth) / (16.0 / 9.0))

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

	frame := tracer.NewFrame(imageWidth, imageHeight)

	tracer.Render(frame, cam, l, 200, 20)

	return frame
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
    var output = document.getElementById("output");
    var input = document.getElementById("input");
    var ws;
		ws = new WebSocket("{{.}}");
		ws.onopen = function(evt) {
				ws.send("render");
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
});
</script>
</head>
<body>
	<canvas id="canvas" width=1000 height=562></canvas>
</body>
</html>
`))
