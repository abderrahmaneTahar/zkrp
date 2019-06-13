package bulletproofs

import (
	"crypto/rand"
	"errors"
	"github.com/mvdbos/zkpsdk/crypto/p256"
	. "github.com/mvdbos/zkpsdk/util"
	"github.com/mvdbos/zkpsdk/util/bn"
	"math"
	"math/big"
)

var SEEDH = "BulletproofsDoesNotNeedTrustedSetupH"

/*
Bulletproofs parameters.
*/
type bp struct {
	N    int64
	G    *p256.P256
	H    *p256.P256
	Gg   []*p256.P256
	Hh   []*p256.P256
	Zkip bip
}

/*
Bulletproofs proof.
*/
type ProofBP struct {
	V       *p256.P256
	A       *p256.P256
	S       *p256.P256
	T1      *p256.P256
	T2      *p256.P256
	Taux    *big.Int
	Mu      *big.Int
	Tprime  *big.Int
	Proofip proofBip
	Commit  *p256.P256
}

/*
Setup is responsible for computing the common parameters.
*/
func (zkrp *bp) Setup(a, b int64) error {
	var i int64

	zkrp.G = new(p256.P256).ScalarBaseMult(new(big.Int).SetInt64(1))
	zkrp.H, _ = p256.MapToGroup(SEEDH)
	zkrp.N = int64(math.Log2(float64(b)))
	zkrp.Gg = make([]*p256.P256, zkrp.N)
	zkrp.Hh = make([]*p256.P256, zkrp.N)
	i = 0
	for i < zkrp.N {
		zkrp.Gg[i], _ = p256.MapToGroup(SEEDH + "g" + string(i))
		zkrp.Hh[i], _ = p256.MapToGroup(SEEDH + "h" + string(i))
		i = i + 1
	}

	// Setup Inner Product
	_, setupErr := zkrp.Zkip.Setup(zkrp.H, zkrp.Gg, zkrp.Hh, new(big.Int).SetInt64(0))
	if setupErr != nil {
		return setupErr
	}
	return nil
}

/*
Prove computes the ZK proof.
*/
func (zkrp *bp) Prove(secret *big.Int) (ProofBP, error) {
	var (
		i     int64
		sL    []*big.Int
		sR    []*big.Int
		proof ProofBP
	)
	//////////////////////////////////////////////////////////////////////////////
	// First phase
	//////////////////////////////////////////////////////////////////////////////

	// commitment to v and gamma
	gamma, _ := rand.Int(rand.Reader, ORDER)
	V, _ := CommitG1(secret, gamma, zkrp.H)

	// aL, aR and commitment: (A, alpha)
	aL, _ := Decompose(secret, 2, zkrp.N)
	aR, _ := computeAR(aL)
	alpha, _ := rand.Int(rand.Reader, ORDER)
	A := commitVector(aL, aR, alpha, zkrp.H, zkrp.Gg, zkrp.Hh, zkrp.N)

	// sL, sR and commitment: (S, rho)
	rho, _ := rand.Int(rand.Reader, ORDER)
	sL = make([]*big.Int, zkrp.N)
	sR = make([]*big.Int, zkrp.N)
	i = 0
	for i < zkrp.N {
		sL[i], _ = rand.Int(rand.Reader, ORDER)
		sR[i], _ = rand.Int(rand.Reader, ORDER)
		i = i + 1
	}
	S := commitVectorBig(sL, sR, rho, zkrp.H, zkrp.Gg, zkrp.Hh, zkrp.N)

	// Fiat-Shamir heuristic to compute challenges y, z
	y, z, _ := HashBP(A, S)

	//////////////////////////////////////////////////////////////////////////////
	// Second phase
	//////////////////////////////////////////////////////////////////////////////
	tau1, _ := rand.Int(rand.Reader, ORDER) // page 20 from eprint version
	tau2, _ := rand.Int(rand.Reader, ORDER)

	// compute t1: < aL - z.1^n, y^n . sR > + < sL, y^n . (aR + z . 1^n) >
	vz, _ := VectorCopy(z, zkrp.N)
	vy := powerOf(y, zkrp.N)

	// aL - z.1^n
	naL, _ := VectorConvertToBig(aL, zkrp.N)
	aLmvz, _ := VectorSub(naL, vz)

	// y^n .sR
	ynsR, _ := VectorMul(vy, sR)

	// scalar prod: < aL - z.1^n, y^n . sR >
	sp1, _ := ScalarProduct(aLmvz, ynsR)

	// scalar prod: < sL, y^n . (aR + z . 1^n) >
	naR, _ := VectorConvertToBig(aR, zkrp.N)
	aRzn, _ := VectorAdd(naR, vz)
	ynaRzn, _ := VectorMul(vy, aRzn)

	// Add z^2.2^n to the result
	// z^2 . 2^n
	p2n := powerOf(new(big.Int).SetInt64(2), zkrp.N)
	zsquared := bn.Multiply(z, z)
	z22n, _ := VectorScalarMul(p2n, zsquared)
	ynaRzn, _ = VectorAdd(ynaRzn, z22n)
	sp2, _ := ScalarProduct(sL, ynaRzn)

	// sp1 + sp2
	t1 := bn.Add(sp1, sp2)
	t1 = bn.Mod(t1, ORDER)

	// compute t2: < sL, y^n . sR >
	t2, _ := ScalarProduct(sL, ynsR)
	t2 = bn.Mod(t2, ORDER)

	// compute T1
	T1, _ := CommitG1(t1, tau1, zkrp.H)

	// compute T2
	T2, _ := CommitG1(t2, tau2, zkrp.H)

	// Fiat-Shamir heuristic to compute 'random' challenge x
	x, _, _ := HashBP(T1, T2)

	//////////////////////////////////////////////////////////////////////////////
	// Third phase                                                              //
	//////////////////////////////////////////////////////////////////////////////

	// compute bl
	sLx, _ := VectorScalarMul(sL, x)
	bl, _ := VectorAdd(aLmvz, sLx)

	// compute br
	// y^n . ( aR + z.1^n + sR.x )
	sRx, _ := VectorScalarMul(sR, x)
	aRzn, _ = VectorAdd(aRzn, sRx)
	ynaRzn, _ = VectorMul(vy, aRzn)
	// y^n . ( aR + z.1^n sR.x ) + z^2 . 2^n
	br, _ := VectorAdd(ynaRzn, z22n)

	// Compute t` = < bl, br >
	tprime, _ := ScalarProduct(bl, br)

	// Compute taux = tau2 . x^2 + tau1 . x + z^2 . gamma
	taux := bn.Multiply(tau2, bn.Multiply(x, x))
	taux = bn.Add(taux, bn.Multiply(tau1, x))
	taux = bn.Add(taux, bn.Multiply(bn.Multiply(z, z), gamma))
	taux = bn.Mod(taux, ORDER)

	// Compute mu = alpha + rho.x
	mu := bn.Multiply(rho, x)
	mu = bn.Add(mu, alpha)
	mu = bn.Mod(mu, ORDER)

	// Inner Product over (g, h', P.h^-mu, tprime)
	// Compute h'
	hprime := make([]*p256.P256, zkrp.N)
	// Switch generators
	yinv := bn.ModInverse(y, ORDER)
	expy := yinv
	hprime[0] = zkrp.Hh[0]
	i = 1
	for i < zkrp.N {
		hprime[i] = new(p256.P256).ScalarMult(zkrp.Hh[i], expy)
		expy = bn.Multiply(expy, yinv)
		i = i + 1
	}

	// Update Inner Product Proof Setup
	zkrp.Zkip.Hh = hprime
	zkrp.Zkip.Cc = tprime

	commit := commitInnerProduct(zkrp.Gg, hprime, bl, br)
	proofip, _ := zkrp.Zkip.Prove(bl, br, commit)

	proof.V = V
	proof.A = A
	proof.S = S
	proof.T1 = T1
	proof.T2 = T2
	proof.Taux = taux
	proof.Mu = mu
	proof.Tprime = tprime
	proof.Proofip = proofip
	proof.Commit = commit

	return proof, nil
}

/*
Verify returns true if and only if the proof is valid.
*/
func (zkrp *bp) Verify(proof ProofBP) (bool, error) {
	var (
		i      int64
		hprime []*p256.P256
	)
	hprime = make([]*p256.P256, zkrp.N)
	y, z, _ := HashBP(proof.A, proof.S)
	x, _, _ := HashBP(proof.T1, proof.T2)

	// Switch generators
	yinv := bn.ModInverse(y, ORDER)
	expy := yinv
	hprime[0] = zkrp.Hh[0]
	i = 1
	for i < zkrp.N {
		hprime[i] = new(p256.P256).ScalarMult(zkrp.Hh[i], expy)
		expy = bn.Multiply(expy, yinv)
		i = i + 1
	}

	//////////////////////////////////////////////////////////////////////////////
	// Check that tprime  = t(x) = t0 + t1x + t2x^2  ----------  Condition (65) //
	//////////////////////////////////////////////////////////////////////////////

	// Compute left hand side
	lhs, _ := CommitG1(proof.Tprime, proof.Taux, zkrp.H)

	// Compute right hand side
	z2 := bn.Multiply(z, z)
	z2 = bn.Mod(z2, ORDER)
	x2 := bn.Multiply(x, x)
	x2 = bn.Mod(x2, ORDER)

	rhs := new(p256.P256).ScalarMult(proof.V, z2)

	delta := zkrp.delta(y, z)

	gdelta := new(p256.P256).ScalarBaseMult(delta)

	rhs.Multiply(rhs, gdelta)

	T1x := new(p256.P256).ScalarMult(proof.T1, x)
	T2x2 := new(p256.P256).ScalarMult(proof.T2, x2)

	rhs.Multiply(rhs, T1x)
	rhs.Multiply(rhs, T2x2)

	// Subtract lhs and rhs and compare with poitn at infinity
	lhs.Neg(lhs)
	rhs.Multiply(rhs, lhs)
	c65 := rhs.IsZero() // Condition (65), page 20, from eprint version

	// Compute P - lhs  #################### Condition (66) ######################

	// S^x
	Sx := new(p256.P256).ScalarMult(proof.S, x)
	// A.S^x
	ASx := new(p256.P256).Add(proof.A, Sx)

	// g^-z
	mz := bn.Sub(ORDER, z)
	vmz, _ := VectorCopy(mz, zkrp.N)
	gpmz, _ := VectorExp(zkrp.Gg, vmz)

	// z.y^n
	vz, _ := VectorCopy(z, zkrp.N)
	vy := powerOf(y, zkrp.N)
	zyn, _ := VectorMul(vy, vz)

	p2n := powerOf(new(big.Int).SetInt64(2), zkrp.N)
	zsquared := bn.Multiply(z, z)
	z22n, _ := VectorScalarMul(p2n, zsquared)

	// z.y^n + z^2.2^n
	zynz22n, _ := VectorAdd(zyn, z22n)

	lP := new(p256.P256)
	lP.Add(ASx, gpmz)

	// h'^(z.y^n + z^2.2^n)
	hprimeexp, _ := VectorExp(hprime, zynz22n)

	lP.Add(lP, hprimeexp)

	// Compute P - rhs  #################### Condition (67) ######################

	// h^mu
	rP := new(p256.P256).ScalarMult(zkrp.H, proof.Mu)
	rP.Multiply(rP, proof.Commit)

	// Subtract lhs and rhs and compare with poitn at infinity
	lP = lP.Neg(lP)
	rP.Add(rP, lP)
	c67 := rP.IsZero()

	// Verify Inner Product Proof ################################################
	ok, _ := zkrp.Zkip.Verify(proof.Proofip)

	result := c65 && c67 && ok

	return result, nil
}

/*
aR = aL - 1^n
*/
func computeAR(x []int64) ([]int64, error) {
	var (
		i      int64
		result []int64
	)
	result = make([]int64, len(x))
	i = 0
	for i < int64(len(x)) {
		if x[i] == 0 {
			result[i] = -1
		} else if x[i] == 1 {
			result[i] = 0
		} else {
			return nil, errors.New("input contains non-binary element")
		}
		i = i + 1
	}
	return result, nil
}

func commitVectorBig(aL, aR []*big.Int, alpha *big.Int, H *p256.P256, g, h []*p256.P256, n int64) *p256.P256 {
	var (
		i int64
		R *p256.P256
	)
	// Compute h^alpha.vg^aL.vh^aR
	R = new(p256.P256).ScalarMult(H, alpha)
	i = 0
	for i < n {
		R.Multiply(R, new(p256.P256).ScalarMult(g[i], aL[i]))
		R.Multiply(R, new(p256.P256).ScalarMult(h[i], aR[i]))
		i = i + 1
	}
	return R
}

/*
Commitvector computes a commitment to the bit of the secret.
*/
func commitVector(aL, aR []int64, alpha *big.Int, H *p256.P256, g, h []*p256.P256, n int64) *p256.P256 {
	var (
		i int64
		R *p256.P256
	)
	// Compute h^alpha.vg^aL.vh^aR
	R = new(p256.P256).ScalarMult(H, alpha)
	i = 0
	for i < n {
		gaL := new(p256.P256).ScalarMult(g[i], new(big.Int).SetInt64(aL[i]))
		haR := new(p256.P256).ScalarMult(h[i], new(big.Int).SetInt64(aR[i]))
		R.Multiply(R, gaL)
		R.Multiply(R, haR)
		i = i + 1
	}
	return R
}

/*
delta(y,z) = (z-z^2) . < 1^n, y^n > - z^3 . < 1^n, 2^n >
*/
func (zkrp *bp) delta(y, z *big.Int) *big.Int {
	var (
		result *big.Int
	)
	// delta(y,z) = (z-z^2) . < 1^n, y^n > - z^3 . < 1^n, 2^n >
	z2 := bn.Multiply(z, z)
	z2 = bn.Mod(z2, ORDER)
	z3 := bn.Multiply(z2, z)
	z3 = bn.Mod(z3, ORDER)

	// < 1^n, y^n >
	v1, _ := VectorCopy(new(big.Int).SetInt64(1), zkrp.N)
	vy := powerOf(y, zkrp.N)
	sp1y, _ := ScalarProduct(v1, vy)

	// < 1^n, 2^n >
	p2n := powerOf(new(big.Int).SetInt64(2), zkrp.N)
	sp12, _ := ScalarProduct(v1, p2n)

	result = bn.Sub(z, z2)
	result = bn.Mod(result, ORDER)
	result = bn.Multiply(result, sp1y)
	result = bn.Mod(result, ORDER)
	result = bn.Sub(result, bn.Multiply(z3, sp12))
	result = bn.Mod(result, ORDER)

	return result
}