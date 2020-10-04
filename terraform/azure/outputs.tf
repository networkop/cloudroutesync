output "public_address" {
  value = [azurerm_public_ip.external.ip_address]
}

