// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package secp256r1

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"
)

// Verifies the given signature (r, s) for the given hash and public key (x, y).
func Verify(hash []byte, r, s, x, y *big.Int) bool {
	// Create the public key format
	publicKey := newECDSAPublicKey(x, y)

	// Check if they are invalid public key coordinates
	if publicKey == nil {
		return false
	}

	// Verify the signature with the public key,
	// then return true if it's valid, false otherwise
	return ecdsa.Verify(publicKey, hash, r, s)
}

// newECDSAPublicKey creates an ECDSA P256 public key from the given coordinates
func newECDSAPublicKey(x, y *big.Int) *ecdsa.PublicKey {
	// Check if the given coordinates are valid and in the reference point (infinity)
	if x == nil || y == nil || x.Sign() == 0 && y.Sign() == 0 {
		return nil
	}

	// SEC1 uncompressed form: 0x04 || X(32) || Y(32) for P-256.
	if x.Sign() < 0 || y.Sign() < 0 || x.BitLen() > 256 || y.BitLen() > 256 {
		return nil
	}
	encodedPoint := make([]byte, 65)
	encodedPoint[0] = 0x04
	x.FillBytes(encodedPoint[1:33])
	y.FillBytes(encodedPoint[33:65])

	if _, err := ecdh.P256().NewPublicKey(encodedPoint); err != nil {
		return nil
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}
}
