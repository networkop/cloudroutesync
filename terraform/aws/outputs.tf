output "public_address_router" {
  value = [aws_instance.router-vm.public_ip]
}