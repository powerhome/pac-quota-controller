# DockerHub Integration Guide

This document explains how to set up the required secrets for publishing Docker images to DockerHub as part of the automated release process.

## Required Secrets

To enable DockerHub publishing, you need to set up the following secrets in your GitHub repository:

1. **DOCKERHUB_USERNAME**: Your DockerHub username
2. **DOCKERHUB_TOKEN**: A DockerHub access token (not your password)
3. **COSIGN_PRIVATE_KEY** (Optional): A Cosign private key for signing images
4. **COSIGN_PASSWORD** (Optional): The password for the Cosign private key

## Setting up DockerHub Access Token

1. Log in to your DockerHub account
2. Go to Account Settings > Security
3. Click on "New Access Token"
4. Give it a descriptive name like "GitHub Actions"
5. Copy the generated token

## Adding Secrets to GitHub

1. Go to your GitHub repository
2. Click on "Settings"
3. Navigate to "Secrets and variables" > "Actions"
4. Click on "New repository secret"
5. Add the following secrets:
   - Name: `DOCKERHUB_USERNAME`, Value: your DockerHub username
   - Name: `DOCKERHUB_TOKEN`, Value: the access token generated in the previous step

## Setting up Cosign for Image Signing (Optional)

If you want to sign your Docker images, you need to set up Cosign:

1. Install Cosign:

   ```bash
   brew install cosign
   ```

2. Generate a new key pair:

   ```bash
   cosign generate-key-pair
   ```

3. Add the private key to GitHub secrets:
   - Name: `COSIGN_PRIVATE_KEY`, Value: contents of the private key file
   - Name: `COSIGN_PASSWORD`, Value: the password you entered when creating the key

## Using the Docker Tools

The project includes several tools to help with Docker image management:

### Docker Login

You can log in to DockerHub from your local environment using:

```bash
export DOCKERHUB_USERNAME=your-username
export DOCKERHUB_TOKEN=your-token
make docker-login
```

### Verifying Published Images

After a release, you can verify that images were properly published to both registries:

```bash
# Verify images for version 0.1.0
./hack/verify-docker-images.sh 0.1.0
```

### Testing the Setup

Once you've set up the secrets, create a new tag to trigger the release process:

```bash
git tag -a v0.1.0 -m "Initial release"
git push origin v0.1.0
```

This will trigger the release workflow, which will build and publish Docker images to both GitHub Container Registry and DockerHub.
