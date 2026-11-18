package mobile

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// call runs one local API request in process and returns its response. A
// failure of the handler is reported in the response with a connect code.
func (c *core) call(ctx context.Context, req *mobilev1.CallRequest) *mobilev1.CallResponse {
	if c == nil {
		return failureOf(connect.CodeFailedPrecondition, "mobile: core is not running")
	}
	method := methodName(req.GetMethod())
	payload := req.GetPayload()
	if len(payload) > maxCallBytes {
		return failureOf(connect.CodeResourceExhausted, "mobile: request is too large")
	}

	switch method {
	case "GetStatus":
		call, err := requestOf[localv1.GetStatusRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetStatus(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ListDevices":
		call, err := requestOf[localv1.ListDevicesRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ListDevices(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "RenameDevice":
		call, err := requestOf[localv1.RenameDeviceRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.RenameDevice(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "RevokeDevice":
		call, err := requestOf[localv1.RevokeDeviceRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.RevokeDevice(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "SetDeviceConfirmation":
		call, err := requestOf[localv1.SetDeviceConfirmationRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.SetDeviceConfirmation(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "OpenPairingWindow":
		call, err := requestOf[localv1.OpenPairingWindowRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.OpenPairingWindow(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ExtendPairingWindow":
		call, err := requestOf[localv1.ExtendPairingWindowRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ExtendPairingWindow(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ClosePairingWindow":
		call, err := requestOf[localv1.ClosePairingWindowRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ClosePairingWindow(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ListPairingCandidates":
		call, err := requestOf[localv1.ListPairingCandidatesRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ListPairingCandidates(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "StartSas":
		call, err := requestOf[localv1.StartSasRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.StartSas(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ConfirmSas":
		call, err := requestOf[localv1.ConfirmSasRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ConfirmSas(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "RejectSas":
		call, err := requestOf[localv1.RejectSasRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.RejectSas(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetPairingQr":
		call, err := requestOf[localv1.GetPairingQrRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetPairingQr(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ScanPairingQr":
		call, err := requestOf[localv1.ScanPairingQrRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ScanPairingQr(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetClip":
		call, err := requestOf[localv1.GetClipRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetClip(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "SetClip":
		call, err := requestOf[localv1.SetClipRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.SetClip(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ListHistory":
		call, err := requestOf[localv1.ListHistoryRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ListHistory(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetHistoryItem":
		call, err := requestOf[localv1.GetHistoryItemRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetHistoryItem(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "DeleteHistoryItem":
		call, err := requestOf[localv1.DeleteHistoryItemRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.DeleteHistoryItem(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ClearHistory":
		call, err := requestOf[localv1.ClearHistoryRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ClearHistory(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "FetchHistoryItem":
		call, err := requestOf[localv1.FetchHistoryItemRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.FetchHistoryItem(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "CopyHistoryItem":
		call, err := requestOf[localv1.CopyHistoryItemRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.CopyHistoryItem(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ApplyHistoryItem":
		call, err := requestOf[localv1.ApplyHistoryItemRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ApplyHistoryItem(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetPause":
		call, err := requestOf[localv1.GetPauseRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetPause(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "SetPause":
		call, err := requestOf[localv1.SetPauseRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.SetPause(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetSettings":
		call, err := requestOf[localv1.GetSettingsRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetSettings(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "UpdateSettings":
		call, err := requestOf[localv1.UpdateSettingsRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.UpdateSettings(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "GetRelay":
		call, err := requestOf[localv1.GetRelayRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.GetRelay(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "SetRelay":
		call, err := requestOf[localv1.SetRelayRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.SetRelay(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "RelayOff":
		call, err := requestOf[localv1.RelayOffRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.RelayOff(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "TestRelay":
		call, err := requestOf[localv1.TestRelayRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.TestRelay(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "SendFiles":
		call, err := requestOf[localv1.SendFilesRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.SendFiles(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ListTransfers":
		call, err := requestOf[localv1.ListTransfersRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ListTransfers(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "CancelTransfer":
		call, err := requestOf[localv1.CancelTransferRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.CancelTransfer(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "AcceptTransfer":
		call, err := requestOf[localv1.AcceptTransferRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.AcceptTransfer(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "RejectTransfer":
		call, err := requestOf[localv1.RejectTransferRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.RejectTransfer(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "ListRequests":
		call, err := requestOf[localv1.ListRequestsRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.ListRequests(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "DecideRequest":
		call, err := requestOf[localv1.DecideRequestRequest](payload)
		if err != nil {
			return invalidRequest(err)
		}
		res, err := c.api.DecideRequest(ctx, call)
		if err != nil {
			return failure(err)
		}
		return encodeResponse(res.Msg)
	case "WatchEvents":
		return failureOf(connect.CodeUnimplemented, "mobile: events are delivered through Platform.OnEvent")
	case "":
		return failureOf(connect.CodeInvalidArgument, "mobile: method is required")
	default:
		return failureOf(connect.CodeUnimplemented, "mobile: unknown method "+method)
	}
}

// methodName returns the RPC name of a local API path.
func methodName(method string) string {
	if index := strings.LastIndex(method, "/"); index >= 0 {
		return method[index+1:]
	}
	return method
}

// requestOf decodes the payload of one call.
func requestOf[M any, P interface {
	*M
	proto.Message
}](payload []byte) (*connect.Request[M], error) {
	msg := P(new(M))
	if len(payload) > 0 {
		if err := proto.Unmarshal(payload, msg); err != nil {
			return nil, err
		}
	}
	return connect.NewRequest(msg), nil
}

// encodeResponse serializes the response of a handler.
func encodeResponse(msg proto.Message) *mobilev1.CallResponse {
	data, err := proto.Marshal(msg)
	if err != nil {
		return failureOf(connect.CodeInternal, "mobile: encode response: "+err.Error())
	}
	if len(data) > maxCallBytes {
		return failureOf(connect.CodeResourceExhausted, "mobile: response is too large")
	}
	return &mobilev1.CallResponse{Payload: data}
}

// invalidRequest reports a payload that does not decode.
func invalidRequest(err error) *mobilev1.CallResponse {
	return failureOf(connect.CodeInvalidArgument, "mobile: decode request: "+err.Error())
}

// failure reports an error of the core with its connect code.
func failure(err error) *mobilev1.CallResponse {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return &mobilev1.CallResponse{
			Code:    int32(connectErr.Code()),
			Message: connectErr.Message(),
		}
	}
	return failureOf(connect.CodeInternal, err.Error())
}

// failureOf builds a response with the given code and message.
func failureOf(code connect.Code, message string) *mobilev1.CallResponse {
	return &mobilev1.CallResponse{Code: int32(code), Message: message}
}
