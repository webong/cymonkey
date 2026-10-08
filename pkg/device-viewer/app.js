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
    await viewer.connect({keyboard:element('keyboard').checked,microphone:element('microphone').checked,pointer:element('pointer').checked});
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
element('pointer').onchange=()=>{element('video').controls=!element('pointer').checked;};
element('video').addEventListener('click',event=>{
  if(!element('pointer').checked)return;
  const point=videoPoint(event);if(point)try{viewer.click(point.x,point.y);}catch(error){report(error);}
});
element('video').addEventListener('contextmenu',event=>{
  if(!element('pointer').checked)return;event.preventDefault();
  const point=videoPoint(event);if(point)try{viewer.click(point.x,point.y,'right');}catch(error){report(error);}
});
element('video').addEventListener('wheel',event=>{
  if(!element('pointer').checked)return;event.preventDefault();
  const point=videoPoint(event);if(point)try{viewer.scroll(point.x,point.y,Math.sign(event.deltaX)*3,-Math.sign(event.deltaY)*3);}catch(error){report(error);}
},{passive:false});

element('diagnostics').onclick=async()=>{try{element('evidence').textContent=JSON.stringify(await viewer.diagnostics(),null,2);}catch(error){report(error);}};
