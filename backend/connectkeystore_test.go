// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"sync"
	"testing"
	"time"

	keystoremock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore/mocks"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"

	"github.com/stretchr/testify/require"
)

func TestConnectKeystore(t *testing.T) {
	ck := connectKeystore{}
	fingerprint := []byte{1, 2, 3, 4}
	fingerprint2 := []byte{5, 6, 7, 8}

	expectedKeystore := &keystoremock.KeystoreMock{
		RootFingerprintFunc: func() ([]byte, error) {
			return fingerprint, nil
		},
	}

	t.Run("timeout", func(t *testing.T) {
		_, err := ck.connect(nil, fingerprint, time.Millisecond)
		require.Equal(t, errTimeout, err)
	})

	t.Run("already connected", func(t *testing.T) {
		ks, err := ck.connect(expectedKeystore, fingerprint, time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, expectedKeystore, ks)
	})

	t.Run("already connected with any keystore accepted", func(t *testing.T) {
		ks, err := ck.connect(expectedKeystore, nil, time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, expectedKeystore, ks)
	})

	t.Run("canceled", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			ck.cancel(errp.ErrUserAbort)
		}()
		_, err := ck.connect(nil, fingerprint, time.Second)
		require.Equal(t, errp.ErrUserAbort, err)
		wg.Wait()
	})

	t.Run("success", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			ck.onConnect(expectedKeystore)
		}()
		ks, err := ck.connect(nil, fingerprint, time.Second)
		require.NoError(t, err)
		require.Equal(t, expectedKeystore, ks)
		wg.Wait()
	})

	t.Run("success with any keystore accepted", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			ck.onConnect(expectedKeystore)
		}()
		ks, err := ck.connect(nil, nil, time.Second)
		require.NoError(t, err)
		require.Equal(t, expectedKeystore, ks)
		wg.Wait()
	})

	t.Run("already connecting", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			ck.onConnect(expectedKeystore)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Previous prompt is canceled by new prompt.
			_, err := ck.connect(nil, fingerprint2, time.Minute)
			require.Equal(t, errReplaced, err)
		}()

		time.Sleep(25 * time.Millisecond)
		_, err := ck.connect(nil, fingerprint, time.Second)
		require.NoError(t, err)
		wg.Wait()
	})

	t.Run("wrong keystore", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			ck.onConnect(expectedKeystore)
		}()
		_, err := ck.connect(nil, fingerprint2, time.Second)
		require.Equal(t, ErrWrongKeystore, err)
		wg.Wait()
	})
}

// A keystore is accepted if its root fingerprint is any of the requested ones, as needed for
// accounts with keys on multiple keystores.
func TestConnectKeystoreAnyOf(t *testing.T) {
	ck := connectKeystore{}
	fingerprint := []byte{1, 2, 3, 4}
	otherFingerprint := []byte{5, 6, 7, 8}
	thirdFingerprint := []byte{9, 9, 9, 9}
	connectedKeystore := &keystoremock.KeystoreMock{
		RootFingerprintFunc: func() ([]byte, error) {
			return fingerprint, nil
		},
	}

	ks, err := ck.connectAnyOf(
		connectedKeystore, [][]byte{otherFingerprint, fingerprint}, time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, connectedKeystore, ks)

	_, err = ck.connectAnyOf(
		connectedKeystore, [][]byte{otherFingerprint, thirdFingerprint}, time.Millisecond)
	require.Equal(t, ErrWrongKeystore, errp.Cause(err))

	// A keystore that connects later is matched against all requested fingerprints.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ks, err := ck.connectAnyOf(nil, [][]byte{otherFingerprint, fingerprint}, time.Second)
		require.NoError(t, err)
		require.Equal(t, connectedKeystore, ks)
	}()
	require.Eventually(t, func() bool {
		defer ck.RLock()()
		return ck.connectKeystoreCallback != nil
	}, time.Second, time.Millisecond)
	ck.onConnect(connectedKeystore)
	wg.Wait()
}
