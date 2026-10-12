// Synthetic fixture only. Production Go binaries do not start Node or load SDK JS.
import {createRequire} from 'node:module';
import http from 'node:http';
const root=process.env.PASEO_NATIVE_NODE_MODULES;
const require=createRequire(`${root}/@getpaseo/relay/package.json`);
const {WebSocketServer}=require('ws');
const {generateKeyPair,exportPublicKey}=await import(`${root}/@getpaseo/relay/dist/crypto.js`);
const {createDaemonChannel}=await import(`${root}/@getpaseo/relay/dist/encrypted-channel.js`);
const {WSInboundMessageSchema}=await import(`${root}/@getpaseo/protocol/dist/messages.js`);
const key=generateKeyPair();const serverId='synthetic-node';
const server=http.createServer((req,res)=>{if(req.url!='/api/v1/native/hosts'||req.headers.authorization!=='Bearer synthetic-owner'){res.writeHead(403);res.end();return}res.setHeader('Content-Type','application/json');res.end(JSON.stringify({ok:true,data:{hosts:{test:{serverId}}}}));});
const wss=new WebSocketServer({noServer:true});
server.on('upgrade',(req,socket,head)=>{if(req.headers.authorization!=='Bearer synthetic-owner'||req.headers['x-paseo-password']){socket.destroy();return}wss.handleUpgrade(req,socket,head,ws=>wss.emit('connection',ws));});
wss.on('connection',ws=>{
 let channel;const transport={send(data){ws.send(data)},close(code,reason){ws.close(code,reason)}};
 ws.on('message',(data,isBinary)=>transport.onmessage?.({data:isBinary?data.buffer.slice(data.byteOffset,data.byteOffset+data.byteLength):data.toString(),isBinary}));ws.on('close',(code,reason)=>transport.onclose?.(code,reason.toString()));ws.on('error',err=>transport.onerror?.(err));
 createDaemonChannel(transport,key,{onmessage(raw){try{
 const text=typeof raw==='string'?raw:new TextDecoder().decode(raw);const request=WSInboundMessageSchema.parse(JSON.parse(text));
 if(request.type==='hello'){if(request.auth?.password!=='synthetic-node-password')throw Error('password must be encrypted hello');void channel.send(JSON.stringify({type:'session',message:{type:'status',payload:{status:'server_info',serverId}}}));return}
 if(request.type!=='session')throw Error('expected RPC envelope');const m=request.message;let type;let payload={requestId:m.requestId};
 if(m.type==='fetch_agent_request'){type='fetch_agent_response';payload.agent={id:m.agentId,status:'idle',pendingPermissions:[]};payload.error=null}
 else if(m.type==='wait_for_finish_request'){type='wait_for_finish_response';Object.assign(payload,{status:'idle',final:{id:m.agentId,status:'idle'},error:null,lastMessage:'synthetic private text'})}
 else if(m.type==='send_agent_message_request'){if(m.text!=='SYNTHETIC-PROMPT'||!m.messageId)throw Error('synthetic message only');type='send_agent_message_response';payload.accepted=true;payload.agentId=m.agentId}
 else throw Error('unexpected synthetic RPC');
 void channel.send(JSON.stringify({type:'session',message:{type,payload}}));
 }catch{transport.close(1008,'Invalid synthetic protocol')}}}).then(value=>{channel=value}).catch(()=>ws.close());
});
server.listen(0,'127.0.0.1',()=>console.log(JSON.stringify({url:`http://127.0.0.1:${server.address().port}`,daemonPublicKeyB64:exportPublicKey(key.publicKey),serverId})));
