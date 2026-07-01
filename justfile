# default: list recipes
default:
    @just --list

build:
    go build -o docaudit .

test:
    go test ./...

install:
    go install .
