FROM nvidia/cuda:12.2.2-devel-ubuntu22.04

RUN apt update && DEBIAN_FRONTEND=noninteractive apt install -y \
        bash git make vim wget g++

ENV GOLANG_VERSION 1.20
RUN wget -nv -O - https://storage.googleapis.com/golang/go${GOLANG_VERSION}.linux-amd64.tar.gz \
    | tar -C /usr/local -xz
ENV PATH /usr/local/go/bin:$PATH

WORKDIR /app

LABEL org.opencontainers.image.description="Speech-to-Text."
LABEL org.opencontainers.image.licenses=MIT

COPY . .

RUN make clone

WORKDIR /app/third_party/whisper.cpp

RUN make clean

RUN WHISPER_CUBLAS=1 make -j libwhisper.a

WORKDIR /app/third_party/whisper.cpp/
RUN bash ./models/download-ggml-model.sh small

WORKDIR /app

RUN mv /app/third_party/whisper.cpp/models/ggml-*.bin ./models/ggml-model.bin

RUN make build && mv bin/transcriber /bin/ && rm -rf bin

ENTRYPOINT [ "/bin/transcriber" ]
