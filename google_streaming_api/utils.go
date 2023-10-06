package google_streaming_api

import (
	"github.com/golang/protobuf/proto"
)

type RecognitionMessage struct {
	event *SpeechRecognitionEvent
}

func NewRecognitionMessage() RecognitionMessage {
	return RecognitionMessage{ event: &SpeechRecognitionEvent{}, }
}

func (rm *RecognitionMessage) Add(text string, final bool) {
	stability := float32(1.0)

	result := &SpeechRecognitionResult{
		Alternative: []*SpeechRecognitionAlternative{
			&SpeechRecognitionAlternative { Transcript: &text, },
		},
       		Stability: &stability,
	        Final: &final,
	}

	rm.event.Result = append(rm.event.Result, result)
}

func (rm *RecognitionMessage) Serialize() ([]byte, error) {
	return proto.Marshal(rm.event)
}
