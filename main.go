package main

import "github.com/networkop/cloudroutesync/cmd"

func main() {
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}
