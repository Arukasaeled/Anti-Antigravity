package supervisor

import "testing"

func TestManagedCDPRejectsAnotherLiveInstance(t *testing.T) {
	owners := map[int]int{57979: 34888, 57772: 41900}
	owner := func(port int) int { return owners[port] }
	if got := resolveCDPPortForHost(41900, 0, []int{57979, 57772}, owner); got != 57772 {
		t.Fatalf("managed host must not read the official instance: got %d", got)
	}
	if got := resolveCDPPortForHost(41900, 57979, []int{57772}, owner); got != 0 {
		t.Fatalf("a reassigned advertised port must fail closed: got %d", got)
	}
	if got := resolveCDPPortForHost(41900, 0, []int{57979}, owner); got != 0 {
		t.Fatalf("not-ready managed host must not borrow another instance: got %d", got)
	}
}
