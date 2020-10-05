output "public_address_router" {
  value = [azurerm_public_ip.external_router.ip_address]
}

output "public_address_vm" {
  value = [azurerm_public_ip.external.ip_address]
}
