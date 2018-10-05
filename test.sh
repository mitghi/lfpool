#!/bin/bash

go test . && go test -race .
#go test -run=^$ -bench=. -cpuprofile=cpu.out
