package main

import (
	"net/http"
        "strconv"
        "os"
	"sync"
	"time"
	"reflect"
	"encoding/binary"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

  	"azul3d.org/engine/audio"
	 _ "azul3d.org/engine/audio/flac"  

	"github.com/boocmp/transcriber/whisper"
	"github.com/boocmp/transcriber/google_streaming_api"
)

// Version set at compile-time
var (
	Version string
)

func main() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = log.Output(
		zerolog.ConsoleWriter{
			Out:     os.Stderr,
			NoColor: true,
		},
	)
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		short := file
		for i := len(file) - 1; i > 0; i-- {
			if file[i] == '/' {
				short = file[i+1:]
				break
			}
		}
		file = short
		return file + ":" + strconv.Itoa(line)
	}

	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = log.With().Caller().Logger()


	app := cli.NewApp()
	app.Name = "Speech-to-Text Using Whisper API"
	app.Usage = "Speech-to-Text."
	app.Action = run
	app.Version = Version

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("can't run app")
	}
}

var model *whisper.WhisperModel

type Handler struct {
	Results chan whisper.TextResult
	Done chan bool
	Transcriber *whisper.Transcriber
	Paired chan bool
}

func (h *Handler)IsPaired()(bool) {
	return h.Results != nil && h.Transcriber != nil
}

type Handlers struct {
	sync.Mutex
	handlers map[string]*Handler
}

func (hs *Handlers)IsLocked() bool {
	const mutexLocked = 1
	state := reflect.ValueOf(hs).Elem().FieldByName("state")
	return state.Int()&mutexLocked == mutexLocked
	return true
}

func (hs *Handlers)CreateHandlerForRequest(req *http.Request) (*Handler, string) {
	if !hs.IsLocked() {
		panic("Lock before calling this function")
	}

	is_upstream_request := req.URL.Path == "/up"
	is_downstream_request := req.URL.Path == "/down"

	log.Info().Msgf("%s", req.URL.Path)

	if !is_upstream_request && !is_downstream_request {
		return nil, ""
	}

	if req.ParseForm() != nil {
		return nil, ""
	}

	pair := req.FormValue("pair")
	if pair == "" {
		return nil, ""
	}

	handler, ok := hs.handlers[pair]
	if !ok {
		handler = new(Handler)
		hs.handlers[pair] = handler
	}

	if is_upstream_request {
		lang := req.FormValue("lang")
		if len(lang) > 2 {
			lang = lang[:2]
		}

		transcriber, err := whisper.NewTranscriber(model, lang)
		if err != nil {
			if hs.handlers[pair] != nil {
				delete(hs.handlers, pair)
			}
			return nil, ""
		}
		handler.Transcriber = transcriber
		if handler.Paired == nil {
			handler.Paired = make(chan bool, 1)
		}
	}
	if is_downstream_request {
		handler.Results = make(chan whisper.TextResult, 1)
		handler.Done = make(chan bool, 2)
		if handler.Paired == nil {
			handler.Paired = make(chan bool)
		}
	}                         
	if handler.IsPaired() {
		if hs.handlers[pair] != nil {
			hs.handlers[pair].Paired <- true
			delete(hs.handlers, pair)
		}
	}

	return handler, pair
}

var handlers Handlers

func run(c* cli.Context) error {
	if m, err := whisper.LoadWhisperModel("/app/models/ggml-model.bin"); err != nil {
		return err
        } else {
		model = m
	}

	handlers = Handlers{ handlers: make(map[string]*Handler) }
	
	http.HandleFunc("/up", handleUpstream)
	http.HandleFunc("/down", handleDownstream)

	http.ListenAndServe(":8090", nil)

	return nil
}

func handleUpstream(w http.ResponseWriter, req *http.Request) {
	handlers.Lock()
	handler, pair := handlers.CreateHandlerForRequest(req)
	if handler == nil {
		// Invalid request, just leave
		handlers.Unlock()
		return
	}
	handlers.Unlock()

	go func() {
		handler.Transcriber.Process()
	}()

	log.Info().Msgf("[UPSTREAM] Start with pair %s", pair)

 	dec, _, _ := audio.NewDecoder(req.Body)

	for quit := false; !quit; {
		select {
			case <-handler.Done:
				quit = true
			default:
				samples := make(audio.Float32, 16000)
				n, err := dec.Read(samples)
				if n <= 0 || err != nil {
					quit = true
					handler.Transcriber.Quit <- true
					<- handler.Done
				} else {
					log.Info().Msgf("Passing = %d to pair=%s",len(samples), pair)
					handler.Transcriber.Audio <- samples
				}
		}
	}

	handler.Transcriber.Quit <- true

	log.Info().Msgf("[UPSTREAM] Done with pair %s", pair)
}

func handleDownstream(w http.ResponseWriter, req *http.Request) {
	handlers.Lock()
	handler, pair := handlers.CreateHandlerForRequest(req)
	if handler == nil {
		// Invalid request, just leave
		handlers.Unlock()
		return
	}
	handlers.Unlock()

	log.Info().Msgf("[DOWNSTREAM] Start with pair %s", pair)


	select {
		case <-handler.Paired:
			break
		case <-time.After(time.Second * 5):
			log.Info().Msgf("[DOWNSTREAM] Timeout upstream connection with pair %s", pair)
			handler.Done <- true
			return
	}

	for running := true; running; {
		select {
			case <-handler.Done:
				running = false
			case <-handler.Transcriber.Done:
				running = false

			case <-time.After(time.Second * 60):
				log.Info().Msgf("Timeout pair=%s", pair)
				running = false

			case result := <-handler.Transcriber.Results:
				message := google_streaming_api.NewRecognitionMessage()
				for _, r := range(result) {
					message.Add(r.Text, r.Final)
				}
				
				bytes, err := message.Serialize()
				if err == nil {
					binary.Write(w, binary.BigEndian, uint32(len(bytes)))
					w.Write(bytes)
					if fl, ok := w.(http.Flusher); ok {
						fl.Flush()
					}
				}
		}
	}

	handler.Done <- true

	log.Info().Msgf("[DOWNSTREAM] Done with pair %s", pair)
}
