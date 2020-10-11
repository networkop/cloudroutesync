output "public_address_router" {
  value = [google_compute_instance.router_vm.network_interface.0.access_config.0.nat_ip]
}

output "public_address_vm" {
  value = [google_compute_instance.normal_vm.network_interface.0.access_config.0.nat_ip]
}
