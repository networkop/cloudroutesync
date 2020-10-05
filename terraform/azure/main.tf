provider "azurerm" {
  version = "=2.20.0"
  features {}
}

locals {
  custom_data = <<CUSTOM_DATA
#!/bin/bash
wget https://golang.org/dl/go1.15.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.15.2.linux-amd64.tar.gz
echo 'PATH=$PATH:/usr/local/go/bin' >> /home/example/.profile

GOPATH=/home/example /usr/local/go/bin/go get -v github.com/networkop/cloudroutersync

sudo apt-get update
sudo apt-get install -y \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg-agent \
    software-properties-common
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository \
  "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) \
  stable"
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io
sudo systemctl restart docker

sudo docker pull frrouting/frr:v7.4.0
sudo curl -L "https://github.com/docker/compose/releases/download/1.27.4/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose


wget https://raw.githubusercontent.com/networkop/cloudroutersync/main/demo-docker/bgpd.conf
wget https://raw.githubusercontent.com/networkop/cloudroutersync/main/demo-docker/daemons
wget https://raw.githubusercontent.com/networkop/cloudroutersync/main/demo-docker/docker-compose.yml


CUSTOM_DATA
}

resource "azurerm_resource_group" "main" {
  name     = "${var.prefix}-resources"
  location = "West Europe"
}

resource "azurerm_virtual_network" "main" {
  name                = "${var.prefix}-network"
  address_space       = ["10.0.0.0/16"]
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
}

resource "azurerm_subnet" "internal" {
  name                 = "internal"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = ["10.0.1.0/24"]
}

resource "azurerm_public_ip" "external_router" {
  name                = "${var.prefix}-public-router-ip"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  allocation_method   = "Static"
}

resource "azurerm_network_interface" "router" {
  name                 = "${var.prefix}-router-nic"
  location             = azurerm_resource_group.main.location
  resource_group_name  = azurerm_resource_group.main.name

  enable_ip_forwarding = true

  ip_configuration {
    name                          = "router-nic"
    subnet_id                     = azurerm_subnet.internal.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.external_router.id
  }
}

resource "azurerm_linux_virtual_machine" "router" {
  name                  = "${var.prefix}-router-vm"
  location              = azurerm_resource_group.main.location
  resource_group_name   = azurerm_resource_group.main.name
  network_interface_ids = [azurerm_network_interface.router.id]
  size                  = "Standard_B1ms"
  admin_username        = var.username
  
  admin_ssh_key {
    username   = var.username
    public_key = file(var.pub_ssh_key_path)
  }
  
  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    offer     = "UbuntuServer"
    publisher = "Canonical"
    sku       = "16.04-LTS"
    version   = "latest"
  }

  identity {
    type = "SystemAssigned"
  }

  custom_data = base64encode(local.custom_data)
}

resource "azurerm_role_assignment" "network" {
  scope                = azurerm_resource_group.main.id
  role_definition_name = "Network Contributor"
  principal_id         = azurerm_linux_virtual_machine.router.identity[0].principal_id
}


######################
# Non-router VM      #
######################
resource "azurerm_public_ip" "external" {
  name                = "${var.prefix}-public-ip"
  resource_group_name = azurerm_resource_group.main.name
  location            = azurerm_resource_group.main.location
  allocation_method   = "Static"
}

resource "azurerm_network_interface" "main" {
  name                 = "${var.prefix}-nic"
  location             = azurerm_resource_group.main.location
  resource_group_name  = azurerm_resource_group.main.name

  enable_ip_forwarding = true

  ip_configuration {
    name                          = "nic"
    subnet_id                     = azurerm_subnet.internal.id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = azurerm_public_ip.external.id
  }
}

resource "azurerm_linux_virtual_machine" "main" {
  name                  = "${var.prefix}-vm"
  location              = azurerm_resource_group.main.location
  resource_group_name   = azurerm_resource_group.main.name
  network_interface_ids = [azurerm_network_interface.main.id]
  size                  = "Standard_B1ms"
  admin_username        = var.username
  
  admin_ssh_key {
    username   = var.username
    public_key = file(var.pub_ssh_key_path)
  }
  
  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    offer     = "UbuntuServer"
    publisher = "Canonical"
    sku       = "16.04-LTS"
    version   = "latest"
  }

  custom_data = base64encode(local.custom_data)

}

