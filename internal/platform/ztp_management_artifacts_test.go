package platform
import("testing";"github.com/enzoOvOyu/keel/internal/fabricboot")
func TestZTPPersonalizedManifestFailsClosed(t *testing.T){
 preview:=&ZTPManagementArtifactPreview{Scripts:[]fabricboot.ManagementScript{{SwitchID:"one",Body:"approved-personalized"}}}
 for _,id:=range []string{"","other"}{if _,e:=ztpApprovedPersonalizedScript(preview,id);e==nil{t.Fatal("personalized manifest fell back to a different script")}}
 if body,e:=ztpApprovedPersonalizedScript(preview,"one");e!=nil||body!="approved-personalized"{t.Fatal("approved device body unavailable",e)}
 if body,e:=ztpApprovedPersonalizedScript(nil,"");e!=nil||body!=fabricboot.IsolationScriptV1{t.Fatal("explicit isolation policy unavailable",e)}
}
