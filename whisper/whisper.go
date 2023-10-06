package whisper

import (
	"errors"
	"regexp"
	"time"

	"github.com/boocmp/whisper.cpp/bindings/go/pkg/whisper"
	"github.com/rs/zerolog/log"
)

const SAMPLE_RATE = whisper.SampleRate

const min_buffer_seconds = 2
const max_buffer_seconds = 15

const MIN_BUFFER_TIME = min_buffer_seconds * time.Second
const MAX_BUFFER_TIME = max_buffer_seconds * time.Second
const MIN_BUFFER_SIZE = min_buffer_seconds * SAMPLE_RATE
const MAX_BUFFER_SIZE = max_buffer_seconds * SAMPLE_RATE

type Transcriber struct {
	ctx      whisper.Context
	state    whisper.State
	model    *WhisperModel
	data     []float32
	filter   *regexp.Regexp

	Quit       chan bool
	Audio      chan []float32
	Results    chan []TextResult
	Done       chan bool
}

type TextResult struct {
	Text string
	Final bool
}

func NewTranscriber(model *WhisperModel, lang string) (*Transcriber, error) {
	ctx, err := model.Model.NewContext()
	if err != nil {
		return nil, err
	}
	if lang !="" {
        	_ = ctx.SetLanguage(lang)
        }
	ctx.SetSuppressNonSpeechTokens(true)
	ctx.SetThreads(8)
	
        log.Info().Msgf("%s", ctx.SystemInfo())

        return &Transcriber{
			ctx: ctx,
			state: ctx.NewState(),
			model: model,
			Quit: make(chan bool, 1),
			Audio: make(chan []float32, 10),
			Results: make(chan []TextResult, 1),
			Done: make(chan bool, 1),
		}, nil
}

func (t* Transcriber) Process() {
	for quit:= false; !quit; {
		select {
			case <-t.Quit:
				results, err := t.process(true)
				if err == nil {
					t.Results <- results
				}
				quit = true
				t.Done <- true
			case samples := <-t.Audio:								
				t.data = append(t.data, samples...)
				log.Info().Msgf("Whisper buffer size %d", len(t.data))
			default:
				log.Info().Msgf("Whisper processing %d", len(t.data))
				results, err := t.process(false)
				if err == nil {
					t.Results <- results
				} else {
					time.Sleep(1 * time.Second)
				}
		}
	}
}

func (t *Transcriber) process(force_final bool) ([]TextResult, error) {
	buffer_len := len(t.data)
	buffer_time := time.Second * time.Duration(buffer_len / SAMPLE_RATE)


	if buffer_time < MIN_BUFFER_TIME {
		return nil, errors.New("insufficient data")
	}

	var result []TextResult

	if segments, err := t.ctx.Process(t.state, t.data); err != nil {
		return nil,  err
	} else {
		log.Info().Msgf("%d", len(segments))
		for _, segment := range segments {
			text := segment.Text
			if text == "" {
				continue
			}
			log.Info().Msgf(text)
			if !force_final && buffer_time < MAX_BUFFER_TIME {
				result = append(result, TextResult{ Text: " " + text, Final: false, })
			} else {
				result = append(result, TextResult{ Text: " " + text, Final: true, })
			}
		}
	}

	if buffer_time >= MAX_BUFFER_TIME {
		t.data = nil // t.data[buffer_len - MIN_BUFFER_SIZE: buffer_len]
	}

	if result == nil {
		return nil, errors.New("failed to transcribe")
	}

	return result, nil
}


type WhisperModel struct {
       Model    whisper.Model
}

func LoadWhisperModel(modelPath string) (*WhisperModel, error) {
        model, err := whisper.New(modelPath)
	if err != nil {
		return nil, err
	}
	return &WhisperModel{ Model: model }, nil
}

func (wm *WhisperModel) Close() error {
 	return wm.Model.Close()
}

