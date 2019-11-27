package keeper

import (
	"encoding/hex"
	pc "github.com/pokt-network/pocket-core/x/pocketcore/types"
	"github.com/pokt-network/posmint/codec"
	sdk "github.com/pokt-network/posmint/types"
	"github.com/pokt-network/posmint/x/auth"
	"github.com/pokt-network/posmint/x/auth/util"
	"github.com/tendermint/tendermint/node"
	"strconv"
)

func (k Keeper) GenerateProofs(ctx sdk.Context, totalRelays int64, header pc.PORHeader) int64 {
	proofSessBlockContext := ctx.WithBlockHeight(header.SessionBlockHeight + int64(k.ProofWaitingPeriod(ctx))*k.SessionFrequency(ctx)) // next session block hash
	blockHash := hex.EncodeToString(proofSessBlockContext.BlockHeader().GetLastBlockId().Hash)
	proofsHash := hex.EncodeToString(pc.SHA3FromString(blockHash + header.String()))[:16] // makes it unique for each session!
	length := len(proofsHash)
	for i := 0; i < length; i++ {
		res, err := strconv.ParseInt(proofsHash[i:], 16, 64)
		if err != nil {
			panic(err)
		}
		if totalRelays > res {
			return res // todo created on the spot! need to audit. Possible to brute force?
		}
	}
	return 0
}

func (k Keeper) TrucateUnnecessaryProofs(porReq int, por pc.ProofOfRelay) pc.ProofOfRelay {
	kept := por.Proofs[porReq]
	por.Proofs = make([]pc.Proof, 1)
	por.Proofs = append(por.Proofs, kept)
	return por
}

func (k Keeper) AreProofsValid(ctx sdk.Context, verifyServicerPubKey string, por pc.ProofOfRelay) bool {
	if 1 != len(por.Proofs) {
		return false
	}
	reqProof := k.GenerateProofs(ctx, por.TotalRelays, por.PORHeader)
	proof := por.Proofs[0]
	if proof.Index != int(reqProof) {
		return false
	}
	if proof.ServicerPubKey != verifyServicerPubKey {
		return false
	}
	if err := proof.Token.Validate(); err != nil {
		return false
	}
	messageHash := hex.EncodeToString(pc.SHA3FromString(strconv.Itoa(proof.Index) + proof.ServicerPubKey + proof.Token.HashString() + strconv.Itoa(int(proof.SessionBlockHeight)))) // todo standardize
	if err := pc.SignatureVerification(por.Proofs[0].Token.ClientPublicKey, messageHash, proof.Signature); err != nil {
		return false

	}
	return true
}

func (k Keeper) SendProofs(ctx sdk.Context, n *node.Node, pbTx func(cdc *codec.Codec, cliCtx util.CLIContext, txBuilder auth.TxBuilder, truncatedPOR pc.ProofOfRelay) error) { // todo should move tx to keeper?
	// auto send the proofBatch transaction
	if k.IsSessionBlock(ctx) { // todo possible congestion if every node sending some proofs at the same block
		proofs := pc.GetAllProofs()
		for _, por := range (*proofs).M {
			// if proof mature!
			if por.SessionBlockHeight == (ctx.BlockHeight() - (int64(k.ProofWaitingPeriod(ctx)) * k.SessionFrequency(ctx))) {
				porReq := k.GenerateProofs(ctx, por.TotalRelays, por.PORHeader) // gen proofs
				truncatedResult := k.TrucateUnnecessaryProofs(int(porReq), por)
				chainID := n.GenesisDoc().ChainID
				fromAddr := sdk.AccAddress(n.PrivValidator().GetPubKey().Address())
				fee := auth.NewStdFee(9000, sdk.NewCoins(sdk.NewInt64Coin(k.StakeDenom(ctx), 0))) // todo gas
				cliCtx := util.NewCLIContext(n, fromAddr, k.coinbasePassphrase).WithCodec(k.cdc)
				accGetter := auth.NewAccountRetriever(cliCtx)
				err := accGetter.EnsureExists(fromAddr)
				account, err := accGetter.GetAccount(fromAddr)
				if err != nil {
					panic(err)
				}
				txBuilder := auth.TxBuilder{
					auth.DefaultTxEncoder(k.cdc),
					k.keybase,
					account.GetAccountNumber(),
					account.GetSequence(),
					fee.Gas,
					1,
					false,
					chainID,
					"",
					fee.Amount,
					fee.GasPrices(),
				}
				if err = pbTx(k.cdc, cliCtx, txBuilder, truncatedResult); err != nil {
					panic(err)
				}
			}
		}
	}
}

func (k Keeper) GetProofsSummary(ctx sdk.Context, address sdk.ValAddress, header pc.PORHeader) (summary pc.ProofOfRelay) {
	store := ctx.KVStore(k.storeKey)
	res := store.Get(pc.KeyForProofOfRelay(ctx, address, header))
	k.cdc.MustUnmarshalBinaryBare(res, &summary)
	return
}

func (k Keeper) GetAllProofSummaries(ctx sdk.Context, address sdk.ValAddress) (summaries []pc.ProofOfRelay) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, pc.KeyForProofOfRelays(address))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var summary pc.ProofOfRelay
		k.cdc.MustUnmarshalBinaryBare(iterator.Value(), &summary)
		summaries = append(summaries, summary)
	}
	return
}

func (k Keeper) GetAllProofSummariesForApp(ctx sdk.Context, address sdk.ValAddress, appPubKeyHex string) (summaries []pc.ProofOfRelay) {
	store := ctx.KVStore(k.storeKey)
	iterator := sdk.KVStorePrefixIterator(store, pc.KeyForProofOfRelaysApp(address, appPubKeyHex))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		var summary pc.ProofOfRelay
		k.cdc.MustUnmarshalBinaryBare(iterator.Value(), &summary)
		summaries = append(summaries, summary)
	}
	return
}

func (k Keeper) SetProofOfRelay(ctx sdk.Context, address sdk.ValAddress, summary pc.ProofOfRelay) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshalBinaryBare(summary)
	store.Set(pc.KeyForProofOfRelay(ctx, address, summary.PORHeader), bz)
}
