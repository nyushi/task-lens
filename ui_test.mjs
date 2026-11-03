import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';
const context = vm.createContext({});
vm.runInContext(readFileSync(new URL('./ui.js', import.meta.url),'utf8'), context);
const age = context.ageInfo;
test('relative age uses data timestamp and advances without changing it',()=>{
 assert.equal(age(1000,1030).label,'1分未満前');
 assert.equal(age(1000,1120).label,'2分前');
 assert.equal(age(1000,4600).label,'1時間前');
 assert.equal(age(1000,87400).label,'1日前');
 assert.equal(age(1000,4599).old,false);
 assert.equal(age(1000,4600).old,true);
 assert.equal(age(1000,1120,120).old,true);
});
test('unknown and future timestamps never appear as old or negative ages',()=>{
 for(const t of [undefined,null,0,-1,NaN,Infinity,1e20]){assert.equal(age(t,1000).label,'更新時刻不明');assert.equal(age(t,1000).old,false)}
 assert.equal(age(1001,1000).label,'未来の時刻');assert.equal(age(1001,1000).old,false);
});
