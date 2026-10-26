PROJECT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

SRC := $(PROJECT)src
# tests/ is intentionally omitted until the charm test suites land.
ALL := $(SRC)

export PYTHONPATH = $(PROJECT):$(PROJECT)/lib:$(SRC)

update-dependencies:
	uv lock -U --no-cache

lint:
	go vet ./...
	uv tool run ruff check $(ALL)
	uv tool run ruff format --check --diff $(ALL)
	uv run --all-extras ty check $(ALL)

format:
	go fmt ./...
	uv tool run ruff check --fix $(ALL)
	uv tool run ruff format $(ALL)

unit:
	go test -v ./...

clean:
	rm -rf bin
	rm -rf .coverage
	rm -rf .pytest_cache
	rm -rf .ruff_cache
	rm -rf .venv
	rm -rf *.charm
	rm -rf *.rock
	rm -rf **/__pycache__
	rm -rf **/*.egg-info
	rm -rf requirements*.txt
