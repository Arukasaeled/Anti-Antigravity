//go:build windows

package supervisor

// Resolve this exact blob; reading the system credential again would race an
// external tool and could attribute an older snapshot to a newer identity.
func nativeCredentialOwner(raw []byte) string {
	if email := emailFromCredentialPayload(raw); email != "" {
		return email
	}
	v, ok := parseCredentialBlobView(raw)
	if !ok || v.Token == nil {
		return ""
	}
	email, _ := identifyCredentialOwner(v.Token.RefreshToken, v.Token.AccessToken)
	return email
}

func credentialForArchive(raw []byte, email string) ([]byte, error) {
	if _, ok := credentialRestorableEmail(raw); ok {
		return withArchivedCredentialIdentity(raw, nil, email)
	}
	archive, err := ReadVaultCredential(email)
	if err != nil {
		return nil, err
	}
	return withArchivedCredentialIdentity(raw, archive, email)
}

func storeCurrentNativeCredential(email string, raw []byte) error {
	complete, err := credentialForArchive(raw, email)
	if err != nil {
		return err
	}
	_, err = StoreVaultCredential(email, displayNameFromCredentialPayload(complete), complete)
	return err
}
