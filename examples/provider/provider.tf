terraform {
  required_providers {
    kaneo = {
      source = "glitchedmob/kaneo"
    }
  }
}

# Set KANEO_USERNAME to the account email and KANEO_PASSWORD in the environment.
provider "kaneo" {
  endpoint = "https://cloud.kaneo.app/api"
}
