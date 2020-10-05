package main

import "github.com/networkop/cloudroutersync/cmd"

func main() {
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}
