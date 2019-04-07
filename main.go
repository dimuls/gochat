package main

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/dimuls/gochat/chat"
	"github.com/go-redis/redis"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/labstack/gommon/log"
	"golang.org/x/net/websocket"
)

const (
	maxPayloadBytes    = 10000
	maxMessagesPerRoom = 100
)

func main() {

	// Init http server.

	httpServer := echo.New()

	httpServer.Use(middleware.Logger())
	httpServer.Use(middleware.Recover())

	httpServer.Use(middleware.AddTrailingSlashWithConfig(middleware.TrailingSlashConfig{
		RedirectCode: http.StatusPermanentRedirect,
	}))

	httpServer.Logger.SetLevel(log.DEBUG)
	httpServer.Logger.SetPrefix("http server")

	indexTmpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		httpServer.Logger.Fatal("failed to parse index template: " + err.Error())
	}

	httpServer.Renderer = &Template{templates: indexTmpl}

	httpServer.GET("/*", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index", c.Request().RequestURI)
	})

	// Init websocket server.

	wsServer := echo.New()

	wsServer.Use(middleware.Logger())
	wsServer.Use(middleware.Recover())

	wsServer.Use(middleware.AddTrailingSlashWithConfig(
		middleware.TrailingSlashConfig{
			RedirectCode: http.StatusPermanentRedirect,
		}),
	)

	wsServer.Logger.SetLevel(log.DEBUG)
	wsServer.Logger.SetPrefix("websocket server")

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	var redisDB int

	redisDBStr := os.Getenv("REDIS_DB")
	if redisDBStr != "" {
		redisDB, err = strconv.Atoi(redisDBStr)
		if err != nil {
			wsServer.Logger.Fatal("failed to parse REDIS_DB environment variable:" +
				err.Error())
		}
	}

	redisStore := newRedisChatStore(redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	}, maxMessagesPerRoom)

	cht := chat.New(redisStore)

	wsServer.GET("/*", func(c echo.Context) error {
		roomName := c.Request().RequestURI

		websocket.Handler(func(conn *websocket.Conn) {
			defer func() {
				if r := recover(); r != nil {
					c.Logger().Errorf("panic: %s, stack: %s", r,
						string(debug.Stack()))
				}
			}()

			defer func() {
				err := conn.Close()
				if err != nil {
					c.Logger().Error("failed to close websocket connection:" +
						err.Error())
				}
			}()

			conn.MaxPayloadBytes = maxPayloadBytes

			wsConn := &wsConnection{conn}

			cht.NewClient(roomName, wsConn)
			defer func() {
				err := cht.RemoveClient(roomName, wsConn)
				if err != nil {
					c.Logger().Error("failed to remove client from chat " +
						"room: " + err.Error())
				}
			}()

			for {
				var m struct {
					Text string `json:"text"`
				}

				err := websocket.JSON.Receive(conn, &m)
				if err != nil {
					if err == io.EOF {
						return
					}
					c.Logger().Error("failed to receive JSON message from " +
						"websocket connection: " + err.Error())
					return
				}

				wsServer.Logger.Debug("got message: ", m)

				err = cht.NewMessage(roomName, wsConn, m.Text)
				if err != nil {
					c.Logger().Error("failed to create new message in " +
						"chat room: " + err.Error())
					continue
				}
			}

		}).ServeHTTP(c.Response(), c.Request())

		return nil
	})

	// Start servers.

	wg := sync.WaitGroup{}

	go func() {
		wg.Add(1)
		defer wg.Done()

		err := httpServer.Start(":80")
		if err != nil {
			if err == http.ErrServerClosed {
				httpServer.Logger.Info("shutting down")
			} else {
				httpServer.Logger.Error("failed to start: " + err.Error())
			}
		}
	}()

	go func() {
		wg.Add(1)
		defer wg.Done()

		err := wsServer.Start(":3000")
		if err != nil {
			if err == http.ErrServerClosed {
				wsServer.Logger.Info("shutting down")
			} else {
				wsServer.Logger.Error("failed to start: " + err.Error())
			}
		}
	}()

	// Wait for interrupt or terminate signals to shutdown servers.

	quit := make(chan os.Signal)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			httpServer.Logger.Error("failed to shutdown: " + err.Error())
		}
	}

	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := wsServer.Shutdown(ctx); err != nil {
			wsServer.Logger.Error("failed to shutdown: " + err.Error())
		}
	}

	wg.Wait()
}

// language=html
const indexHTML = `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1" />
	<title>{{.}}</title>
	<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/normalize/8.0.1/normalize.min.css"
		integrity="sha256-l85OmPOjvil/SOvVt3HnSSjzF1TUMyT9eV0c2BzEGzU="
		crossorigin="anonymous" />
	<link rel="stylesheet" href="https://use.fontawesome.com/releases/v5.8.1/css/all.css"
		integrity="sha384-50oBUHEmvpQ+1lW4y57PTFmhCaXp0ML5d60M1M7uH2+nqUivzIebhndOJK28anvf"
		crossorigin="anonymous">
<style>
html, body {
	padding: 0;
	margin: 0;
	height: 100%;
	font-size: 16px;
}
body {
	display: flex;
	flex-direction: column;
	align-items: center;
}
.header {
	position: fixed;
	top: 0;
	display: flex;
	flex-direction: row;
	height: 3em;
	background: white;
	width: 100%;
	max-width: 60em;
}
.header__main {
	flex-grow: 1;
	font-size: 2em;
	margin-left: 0.25em;
}
.header__right {
	font-size: 2em;
	margin-right: 0.25em;
}
.header__icon {
	font-size: 0.85em;
}
.messages {
	flex-grow: 2;
	max-width: 60em;
	width: 100%;
	display: flex;
	flex-direction: column;
	padding-top: 3em;
	padding-bottom: 4.9em;
}
.message {
	margin: 0.5em;
}
.editor {
	position: fixed;
	bottom: 0;
	box-sizing: border-box;
	max-width: 60em;
	width: 100%;
	display: flex;
	padding: 0.2em;
}
.editor__input {
	box-sizing: border-box;
	resize: none;
	flex-grow: 1;
	padding: 0.3em;
	border-radius: 0.3em 0 0 0.3em;
	height: 4.5em;
}
.editor__send {
	box-sizing: border-box;
	border-radius: 0 0.3em 0.3em 0;
	padding: 0 1em;
	user-select: none;
	position: relative;
	left: -1px;
}
</style>
</head>
<body>

<div class="header">
	<div class="header__main">{{.}}</div>
	<div class="header__right">
		<i class="fas fa-user header__icon"></i>
		<span data-bind="text: clientsCount"></span>
	</div>
</div>

<div class="messages" data-bind="foreach: messages">
	<div class="message">
		<div>
			<b data-bind="text: '#'+id"></b>
			<span data-bind="text: time"></span>
		</div>
		<div data-bind="html: html"></div>
	</div>
</div>

<div class="editor">
	<textarea class="editor__input" autofocus
		data-bind="textInput: editor.input, onCtrlEnter:editor.send"></textarea>
	<button class="editor__send" data-bind="click: editor.send">send</button>
</div>

<script src="https://cdnjs.cloudflare.com/ajax/libs/knockout/3.5.0/knockout-min.js"
	integrity="sha256-Tjl7WVgF1hgGMgUKZZfzmxOrtoSf8qltZ9wMujjGNQk="
	crossorigin="anonymous"></script>
<script>!function(a,b){"function"==typeof define&&define.amd?define([],b):"undefined"!=typeof module&&module.exports?module.exports=b():a.ReconnectingWebSocket=b()}(this,function(){function a(b,c,d){function l(a,b){var c=document.createEvent("CustomEvent");return c.initCustomEvent(a,!1,!1,b),c}var e={debug:!1,automaticOpen:!0,reconnectInterval:1e3,maxReconnectInterval:3e4,reconnectDecay:1.5,timeoutInterval:2e3};d||(d={});for(var f in e)this[f]="undefined"!=typeof d[f]?d[f]:e[f];this.url=b,this.reconnectAttempts=0,this.readyState=WebSocket.CONNECTING,this.protocol=null;var h,g=this,i=!1,j=!1,k=document.createElement("div");k.addEventListener("open",function(a){g.onopen(a)}),k.addEventListener("close",function(a){g.onclose(a)}),k.addEventListener("connecting",function(a){g.onconnecting(a)}),k.addEventListener("message",function(a){g.onmessage(a)}),k.addEventListener("error",function(a){g.onerror(a)}),this.addEventListener=k.addEventListener.bind(k),this.removeEventListener=k.removeEventListener.bind(k),this.dispatchEvent=k.dispatchEvent.bind(k),this.open=function(b){h=new WebSocket(g.url,c||[]),b||k.dispatchEvent(l("connecting")),(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","attempt-connect",g.url);var d=h,e=setTimeout(function(){(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","connection-timeout",g.url),j=!0,d.close(),j=!1},g.timeoutInterval);h.onopen=function(){clearTimeout(e),(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","onopen",g.url),g.protocol=h.protocol,g.readyState=WebSocket.OPEN,g.reconnectAttempts=0;var d=l("open");d.isReconnect=b,b=!1,k.dispatchEvent(d)},h.onclose=function(c){if(clearTimeout(e),h=null,i)g.readyState=WebSocket.CLOSED,k.dispatchEvent(l("close"));else{g.readyState=WebSocket.CONNECTING;var d=l("connecting");d.code=c.code,d.reason=c.reason,d.wasClean=c.wasClean,k.dispatchEvent(d),b||j||((g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","onclose",g.url),k.dispatchEvent(l("close")));var e=g.reconnectInterval*Math.pow(g.reconnectDecay,g.reconnectAttempts);setTimeout(function(){g.reconnectAttempts++,g.open(!0)},e>g.maxReconnectInterval?g.maxReconnectInterval:e)}},h.onmessage=function(b){(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","onmessage",g.url,b.data);var c=l("message");c.data=b.data,k.dispatchEvent(c)},h.onerror=function(b){(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","onerror",g.url,b),k.dispatchEvent(l("error"))}},1==this.automaticOpen&&this.open(!1),this.send=function(b){if(h)return(g.debug||a.debugAll)&&console.debug("ReconnectingWebSocket","send",g.url,b),h.send(b);throw"INVALID_STATE_ERR : Pausing to reconnect websocket"},this.close=function(a,b){"undefined"==typeof a&&(a=1e3),i=!0,h&&h.close(a,b)},this.refresh=function(){h&&h.close()}}return a.prototype.onopen=function(){},a.prototype.onclose=function(){},a.prototype.onconnecting=function(){},a.prototype.onmessage=function(){},a.prototype.onerror=function(){},a.debugAll=!1,a.CONNECTING=WebSocket.CONNECTING,a.OPEN=WebSocket.OPEN,a.CLOSING=WebSocket.CLOSING,a.CLOSED=WebSocket.CLOSED,a});</script>
<script>
	ko.extenders.scrollFollow = function (target, selector) {
		target.subscribe(function (newval) {
			var el = document.querySelector(selector);
			if (el.scrollTop === el.scrollHeight - el.clientHeight) {
				setTimeout(function () {
				    el.scrollTop = el.scrollHeight - el.clientHeight;
				}, 0);
			}
		});
		return target;
	};
	
	ko.extenders.lengthLimit = function(target, limit) {
	    target.subscribe(function(arr) {
	    	var length = arr.length;
			if (length > limit) {
				target.splice(0, length-limit)
			} 
	    });  
	};

	ko.bindingHandlers.onCtrlEnter = {
		init: function (element, valueAccessor, allBindings, viewModel) {
			var callback = valueAccessor();
			element.onkeypress = function (e) {
				if ((e.ctrlKey || e.metaKey) &&
					(e.keyCode === 13 || e.keyCode === 100)) {
					callback.call(viewModel);
					return false;
				}
				return true;
			};
		}
	};

    var chat = {};

    chat.connected = ko.observable(false);
    chat.messages = ko.observableArray().extend({
    	scrollFollow: 'html',
    	lengthLimit: 10
    });
   	chat.clientsCount = ko.observable(0);
    
    chat.editor = {};
    
    chat.editor.input = ko.observable();
    
    chat.editor.send = function() {
        chat.ws.send(JSON.stringify({
        	text: chat.editor.input()
        }))
    };

    chat.ws = new ReconnectingWebSocket(
        (location.protocol === 'http:' ? 'ws' : 'wss') + '://' +
        location.hostname +
        ':3000' +
        location.pathname
    );

    chat.ws.onopen = function() {
      chat.connected(true);
      console.log('websocket connected');
    };
    
    chat.ws.onclose = function() {
      chat.connected(false);
      console.log("websocket closed")
    };

    chat.ws.onmessage = function(e) {
      console.debug("websocket message received: ", e.data);
      var msg = JSON.parse(e.data);
      switch (msg.type) {
      	case 'message':
			msg = msg.payload;
			if (msg.approve) {
				chat.editor.input('');
			}
			msg.time = new Date(msg.time).toLocaleString();
			chat.messages.push(msg); 
			break;
      	case 'messages':
      	    msgs = msg.payload;
      	    for (var i = 0; i < msgs.length; i++) {
      	        msgs[i].time = new Date(msgs[i].time).toLocaleString(); 
      	    }
      	    chat.messages(msgs); 
      	    break;
      	case 'clients-count':
      	    chat.clientsCount(msg.payload);
      	    break;
      	case 'error':
      	    console.error(msg.payload);
      	    break;
      }
    };

    ko.applyBindings(chat);
</script>
</body>
</html>
`

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string,
	data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type wsConnection struct {
	*websocket.Conn
}

func (c *wsConnection) sendError(err error) {
	_ = websocket.JSON.Send(c.Conn, wsMessage{
		Type:    "error",
		Payload: err.Error(),
	})
}

func (c *wsConnection) SendClientsCount(count uint) {
	_ = websocket.JSON.Send(c.Conn, wsMessage{
		Type:    "clients-count",
		Payload: count,
	})
}

func (c *wsConnection) SendMessage(msg chat.Message) {
	_ = websocket.JSON.Send(c.Conn, wsMessage{
		Type:    "message",
		Payload: msg,
	})
}

func (c *wsConnection) SendMessages(msgs []chat.Message) {
	_ = websocket.JSON.Send(c.Conn, wsMessage{
		Type:    "messages",
		Payload: msgs,
	})
}
