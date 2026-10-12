import {DeviceViewer} from './viewer.js';
const element=id=>document.getElementById(id);
let viewer, muted=false;
const report=error=>{element('status').textContent=error.message ?? String(error);};
element('connect').onsubmit=async event=>{
  event.preventDefault(); viewer?.close();
  try {
    viewer=new DeviceViewer({url:element('endpoint').value,token:element('credential').value,video:element('video'),iceServers:JSON.parse(element('ice').value || '[]'),relayOnly:element('relay').checked});
    element('credential').value=''; element('ice').value=''; element('evidence').textContent='';
    viewer.addEventListener('state',event=>report(event.detail));
    await viewer.connect({keyboard:element('keyboard').checked,microphone:element('microphone').checked,pointer:element('pointer').checked || element('drag').checked});
  } catch(error){report(error);}
};
element('send').onclick=()=>{try{viewer.type(element('text').value);element('text').value='';}catch(error){report(error);}};
element('press').onclick=()=>{try{viewer.press(element('key').value);}catch(error){report(error);}};
element('play').onclick=()=>element('video').play().catch(report);
element('mute').onclick=()=>{muted=!muted;viewer?.muteMicrophone(muted);element('mute').textContent=muted?'Unmute microphone':'Mute microphone';};
element('restart').onclick=()=>viewer?.restart().catch(report);
element('disconnect').onclick=()=>viewer?.close();
window.addEventListener('pagehide',()=>viewer?.close());

// Pointer coordinates are normalized to the displayed image, excluding bars.
function videoPoint(event) {
  const video=element('video'), rect=video.getBoundingClientRect();
  if(!video.videoWidth || !video.videoHeight) return null;
  const scale=Math.min(rect.width/video.videoWidth,rect.height/video.videoHeight);
  const width=video.videoWidth*scale,height=video.videoHeight*scale;
  const x=(event.clientX-rect.left-(rect.width-width)/2)/width;
  const y=(event.clientY-rect.top-(rect.height-height)/2)/height;
  return x>=0&&x<=1&&y>=0&&y<=1 ? {x,y} : null;
}
function pointerModeChanged(){ cancelDrag(); element('video').controls=!(element('pointer').checked||element('drag').checked);element('video').style.touchAction=element('drag').checked?'none':''; }
element('pointer').onchange=pointerModeChanged;
element('drag').onchange=pointerModeChanged;
element('video').addEventListener('click',event=>{
  if(element('drag').checked||!element('pointer').checked)return;
  const point=videoPoint(event);if(point)try{viewer.click(point.x,point.y);}catch(error){report(error);}
});
element('video').addEventListener('contextmenu',event=>{
  if(element('drag').checked||!element('pointer').checked)return;event.preventDefault();
  const point=videoPoint(event);if(point)try{viewer.click(point.x,point.y,'right');}catch(error){report(error);}
});
element('video').addEventListener('wheel',event=>{
  if(element('drag').checked||!element('pointer').checked)return;event.preventDefault();
  const point=videoPoint(event);if(point)try{viewer.scroll(point.x,point.y,Math.sign(event.deltaX)*3,-Math.sign(event.deltaY)*3);}catch(error){report(error);}
},{passive:false});

element('diagnostics').onclick=async()=>{try{element('evidence').textContent=JSON.stringify(await viewer.diagnostics(),null,2);}catch(error){report(error);}};

// Capture the initiating pointer so releasing outside the video ends the hold.
let dragging=null,lastMove=0;
const buttonName=n=>['left','middle','right'][n];
element('video').addEventListener('pointerdown',event=>{
  if(!element('drag').checked||dragging||!buttonName(event.button))return;
  const point=videoPoint(event);if(!point)return;
  event.preventDefault();
  try{viewer.down(point.x,point.y,buttonName(event.button));dragging={id:event.pointerId,button:buttonName(event.button),...point};element('video').setPointerCapture(event.pointerId);}
  catch(error){viewer?.releasePointer();dragging=null;report(error);}
});
element('video').addEventListener('pointermove',event=>{
  if(!dragging||dragging.id!==event.pointerId)return;
  const point=videoPoint(event);if(!point)return;
  dragging={...dragging,...point};
  if(performance.now()-lastMove<50)return;lastMove=performance.now();
  try{viewer.move(point.x,point.y);}catch(error){viewer?.releasePointer();dragging=null;report(error);}
});
element('video').addEventListener('pointerup',event=>{
  if(!dragging||dragging.id!==event.pointerId)return;
  const hold=dragging;dragging=null;const point=videoPoint(event)||hold;
  try{viewer.up(point.x,point.y,hold.button);}catch(error){report(error);}
  if(element('video').hasPointerCapture(event.pointerId))element('video').releasePointerCapture(event.pointerId);
});
function cancelDrag(){dragging=null;viewer?.releasePointer();}
element('video').addEventListener('pointercancel',cancelDrag);
element('video').addEventListener('lostpointercapture',cancelDrag);
window.addEventListener('blur',cancelDrag);
document.addEventListener('visibilitychange',()=>{if(document.hidden)cancelDrag();});
element('video').addEventListener('contextmenu',event=>{if(element('drag').checked)event.preventDefault();});
