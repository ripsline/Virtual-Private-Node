package lndrpc

import "testing"

func TestDisconnectedClientInvoiceMethods(t *testing.T) {
	c := &Client{}
	if _, err := c.AddInvoice(1000, "test", false); err == nil {
		t.Error("should error")
	}
	if _, err := c.DecodePayReq("lnbc..."); err == nil {
		t.Error("should error")
	}
	if _, err := c.LookupInvoice([]byte{1, 2, 3}); err == nil {
		t.Error("should error")
	}
	if _, err := c.ListInvoices(10); err == nil {
		t.Error("should error")
	}
	if _, err := c.ListPayments(10); err == nil {
		t.Error("should error")
	}
	if _, err := c.SendPayment("lnbc..."); err == nil {
		t.Error("should error")
	}
}
