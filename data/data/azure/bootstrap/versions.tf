terraform {
  required_version = ">= 0.14"
  required_providers {
    local = {
      source = "openshift/local/local"
    }
    azurerm = {
      source = "openshift/local/azurerm"
    }
    ignition = {
      source = "openshift/local/ignition"
    }
  }
}

