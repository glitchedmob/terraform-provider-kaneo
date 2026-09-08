terraform {
  required_providers {
    kaneo = {
      source = "glitchedmob/kaneo"
    }
  }
}

# Set KANEO_API_KEY in the environment rather than committing the API key.
provider "kaneo" {
  endpoint = "https://cloud.kaneo.app/api"
}
