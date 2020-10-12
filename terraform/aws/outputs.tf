output "public_address_router" {
  value = [aws_instance.router-vm.public_ip]
}

output "public_address_vm" {
  value = [aws_instance.normal-vm.public_ip]
}