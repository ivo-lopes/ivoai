#include <cpuid.h>
#include <stdio.h>
int main(void) {
    unsigned int a,b,c,d;
    int avx=0,avx2=0,pclmul=0;
    if (__get_cpuid(1,&a,&b,&c,&d)) {
        avx=(c>>28)&1;
        pclmul=(c>>1)&1;
    }
    if (__get_cpuid_count(7,0,&a,&b,&c,&d)) avx2=(b>>5)&1;
    printf("{\"avx\":%d,\"avx2\":%d,\"pclmul\":%d}\n",avx,avx2,pclmul);
    return 0;
}
