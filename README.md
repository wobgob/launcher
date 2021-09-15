# launcher
Wobbling Goblin Launcher

## Building
Create a new file `auth.go` with the following contents:

```go
package main

const (
    accessKeyID = "<access-key>"
    secretAccessKey = "<secret-key>"
)
```

1. `go install github.com/tc-hib/go-winres@latest`
2. `go generate`
3. `go build`

## Running
1. Place `launcher.exe` where you want your 3.3.5a client to be updated or installed.
2. Run `launcher.exe` to download the client, patch the client, and launch the client.

## Self-Signing
1. Generate the key.

```powershell
New-SelfSignedCertificate -DnsName 'email@yourdomain.com' -Type CodeSigning -CertStoreLocation cert:\CurrentUser\My
```

2. Export the certificate with the private key for secure storage.

```powershell
# Password used to protect the private key.
$mypwd = ConvertTo-SecureString -String "1234" -Force -AsPlainText
Export-PfxCertificate -Cert (Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert)[0] -FilePath code_signing.pfx -Password $mypwd
```

The `[0]` will make this work for cases when you have more than one certificate. Obviously make the index match the certificate you want to use or use a way to filtrate (by thumbprint or issuer).

3. Export the certificate without the private key.

```powershell
Export-Certificate -Cert (Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert)[0] -FilePath code_signing.crt
```

4. Import it as Trusted Publisher.

```powershell
Import-Certificate -FilePath .\code_signing.crt -Cert Cert:\CurrentUser\TrustedPublisher
```

5. Import it as a Root Certificate Authority.

```powershell
Import-Certificate -FilePath .\code_signing.crt -Cert Cert:\CurrentUser\Root
```

6. Sign the executable.

```powershell
Set-AuthenticodeSignature .\launcher.exe -Certificate (Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert)[0]
```

7. If you later want to remove the certificate and start again.

```powershell
(Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert)[0] | Remove-Item
```
