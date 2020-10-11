variable "prefix" {
  default = "example"
}

variable "username" {
  default = "example"
}

variable "pub_ssh_key_path" {
  default = "~/.ssh/id_rsa.pub"
}

variable "priv_ssh_key_path" {
  default = "~/.ssh/id_rsa"
}

variable "ubuntu_ami" {
  default = "ami-06fd8a495a537da8b"
}