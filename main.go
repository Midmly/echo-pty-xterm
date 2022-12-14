package main

import (
	"encoding/json"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"io"
	"log"
	"net/http"
	"os/exec"
	"syscall"
	"unsafe"
)

type windowSize struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
	X    uint16
	Y    uint16
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	//l := log.WithField("remoteaddr", r.RemoteAddr)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("remote: %s ,Unable to upgrade connection,err-> %s", r.RemoteAddr, err.Error())
		return
	}

	cmd := exec.Command("bash")
	//cmd.Env = append(os.Environ(), "TERM=xterm")

	tty, err := pty.Start(cmd)
	if err != nil {
		log.Printf("remote: %s ,Unable to start pty/cmd,err-> %s", r.RemoteAddr, err.Error())
		conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		return
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
		tty.Close()
		conn.Close()
	}()

	go func() {
		for {
			buf := make([]byte, 1024)
			read, err := tty.Read(buf)
			if err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
				log.Printf("remote: %s ,Unable to read from pty/cmd,err-> %s", r.RemoteAddr, err.Error())
				return
			}
			conn.WriteMessage(websocket.TextMessage, buf[:read])
		}
	}()

	for {
		messageType, reader, err := conn.NextReader()
		if err != nil {
			log.Printf("remote: %s ,Unable to grab next reader,err-> %s", r.RemoteAddr, err.Error())
			return
		}

		if messageType == websocket.TextMessage {
			log.Printf("remote: %s ,Unexpected text message", r.RemoteAddr)
			conn.WriteMessage(websocket.TextMessage, []byte("Unexpected text message"))
			continue
		}

		dataTypeBuf := make([]byte, 1)
		read, err := reader.Read(dataTypeBuf)
		if err != nil {
			log.Printf("remote: %s ,Unable to read message type from reader,err-> %s", r.RemoteAddr, err.Error())
			conn.WriteMessage(websocket.TextMessage, []byte("Unable to read message type from reader"))
			return
		}

		if read != 1 {
			log.Printf("remote: %s ,Unexpected number of bytes read", r.RemoteAddr)
			return
		}

		switch dataTypeBuf[0] {
		case 0:
			copied, err := io.Copy(tty, reader)
			if err != nil {
				log.Printf("remote: %s ,Error after copying %d bytes,err-> %s", r.RemoteAddr, copied, err.Error())
			}
		case 1:
			decoder := json.NewDecoder(reader)
			resizeMessage := windowSize{}
			err := decoder.Decode(&resizeMessage)
			if err != nil {
				conn.WriteMessage(websocket.TextMessage, []byte("Error decoding resize message: "+err.Error()))
				continue
			}
			log.Printf("remote: %s ,resizeMessage ->Resizing terminal", r.RemoteAddr)
			_, _, errno := syscall.Syscall(
				syscall.SYS_IOCTL,
				tty.Fd(),
				syscall.TIOCSWINSZ,
				uintptr(unsafe.Pointer(&resizeMessage)),
			)
			if errno != 0 {
				log.Printf("remote: %s ,Unable to resize terminal", r.RemoteAddr)
			}
		default:
			log.Printf("remote: %s ,dataType %d ->Unknown data type", r.RemoteAddr, dataTypeBuf[0])
		}
	}
}

func main() {
	e := echo.New()
	e.Use(middleware.Recover())
	e.GET("/system/ws", func(c echo.Context) error {
		HandleWebsocket(c.Response(), c.Request())
		return nil
	})
	log.Printf("listen at 0.0.0.0:8080")
	err := e.Start("0.0.0.0:8080")
	if err != nil {
		log.Println(err.Error())
	}
}
