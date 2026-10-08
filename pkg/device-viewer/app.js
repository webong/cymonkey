import {DeviceViewer} from './viewer.js';
const element=id=>document.getElementById(id);
let viewer, muted=false;
const report=error=>{element('status').textContent=error.message ?? String(error);};
element('connect').onsubmit=async event=>{
  event.preventDefault(); viewer?.close();
  try {
    viewer=new DeviceViewer({url:element('endpoint').value,token:element('credential').value,video:element('video')});
    element('credential').value='';
    viewer.addEventListener('state',event=>report(event.detail));
    await viewer.connect({keyboard:element('keyboard').checked,microphone:element('microphone').checked});
  } catch(error){report(error);}
};
element('send').onclick=()=>{try{viewer.type(element('text').value);element('text').value='';}catch(error){report(error);}};
element('press').onclick=()=>{try{viewer.press(element('key').value);}catch(error){report(error);}};
element('play').onclick=()=>element('video').play().catch(report);
element('mute').onclick=()=>{muted=!muted;viewer?.muteMicrophone(muted);element('mute').textContent=muted?'Unmute microphone':'Mute microphone';};
element('restart').onclick=()=>viewer?.restart().catch(report);
element('disconnect').onclick=()=>viewer?.close();
window.addEventListener('pagehide',()=>viewer?.close());
