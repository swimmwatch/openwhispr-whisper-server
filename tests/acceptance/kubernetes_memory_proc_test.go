package acceptance

// This read-only driver-Pod probe emits only a bounded framed PID list or a
// fixed failure category. Its caller separately checks hostPID, container
// ownership, native NSpid/start time and stable identities before and after.
const p08MemoryNativeProcProbe = `use strict;
use warnings;
use Errno qw(ENOENT EACCES EPERM);

sub failure { my ($s)=@_; print "ERROR:$s\n"; exit 0; }
sub io_failure { return ($! == EACCES || $! == EPERM) ? 'access' : 'io'; }
sub read_bounded {
 my ($path,$limit)=@_;
 open(my $f, '<', $path) or failure(io_failure());
 my $buf='';
 while (1) {
  my $chunk=''; my $n=sysread($f,$chunk,4096);
  defined($n) or failure(io_failure());
  last if $n==0;
  $buf.=$chunk; length($buf)<=$limit or failure('format');
 }
 close($f) or failure(io_failure());
 return $buf;
}
sub mount_identity {
 my ($mountfile)=@_;
 my $data=read_bounded($mountfile,4194304);
 $data =~ /\n\z/ or failure('format');
 my @matches;
 for my $line (split(/\n/,$data)) {
  my ($left,$right)=split(/ - /,$line,2);
  defined($right) or failure('format');
  my @l=split(/ /,$left); my @r=split(/ /,$right);
  @l>=6 && @r>=3 or failure('format');
  $l[4] !~ m{\A/proc/[0-9]+(?:/|\z)} or failure('guard');
  next unless $l[4] eq '/proc';
  $l[3] eq '/' && $r[0] eq 'proc' or failure('guard');
  my $opts=$l[5].','.$r[2];
  $opts !~ /(?:\A|,)hidepid(?:=|,|\z)/ or failure('guard');
  push @matches,$line;
 }
 @matches==1 or failure('guard');
 return $data;
}
sub absent_twice {
 my ($dir)=@_;
 for (1..2) {
  $!=0; my @st=lstat($dir);
  @st and failure('unconfirmed');
  $! == ENOENT or failure(io_failure());
 }
}
sub scan {
 my ($proc,$uid,$mountfile,$uidmap)=@_;
 $uid == 0 or failure('guard');
 my $mapping=read_bounded($uidmap,4096);
 $mapping =~ /\A\s*0\s+0\s+4294967295\s*\z/ or failure('guard');
 my $mount=mount_identity($mountfile);
 my $selfpid=readlink("$proc/self/ns/pid"); defined($selfpid) or failure(io_failure());
 my $initpid=readlink("$proc/1/ns/pid"); defined($initpid) or failure(io_failure());
 $selfpid =~ /\Apid:\[[0-9]+\]\z/ && $selfpid eq $initpid or failure('guard');
 opendir(my $d,$proc) or failure(io_failure());
 my @pids;
 while (1) {
  $! = 0; my $name=readdir($d);
  if (!defined($name)) { $! == 0 or failure(io_failure()); last; }
  next unless $name =~ /\A[0-9]+\z/;
  $name =~ /\A[1-9][0-9]*\z/ or failure('format');
  push @pids,$name; @pids<=65536 or failure('format');
 }
 closedir($d) or failure(io_failure());
 my @native;
 for my $pid (sort { $a <=> $b } @pids) {
  my $dir="$proc/$pid";
  if (!open(my $f, '<', "$dir/comm")) {
   if ($! == ENOENT) { absent_twice($dir); next; }
   failure(io_failure());
  } else {
   my $buf=''; my $vanished=0;
   while (1) {
    my $chunk=''; my $n=sysread($f,$chunk,257);
    if (!defined($n)) {
     if ($! == ENOENT) { absent_twice($dir); $vanished=1; last; }
     failure(io_failure());
    }
    last if $n==0;
    $buf.=$chunk; length($buf)<=256 or failure('format');
   }
   close($f) or failure(io_failure());
   next if $vanished;
   $buf =~ /\A[^\n]*\n\z/ or failure('format');
   push @native,$pid if $buf eq "whisper-server\n";
   @native<=256 or failure('format');
  }
 }
 mount_identity($mountfile) eq $mount or failure('guard');
 my $selfafter=readlink("$proc/self/ns/pid"); defined($selfafter) or failure(io_failure());
 my $initafter=readlink("$proc/1/ns/pid"); defined($initafter) or failure(io_failure());
 $selfafter eq $selfpid && $initafter eq $initpid or failure('guard');
 print "OK\n", join('',map { "$_\n" } @native), "END\n" or failure('io');
}
@ARGV == 1 or failure('guard');
my $root = $ARGV[0];
$root =~ m{\A/} && $root !~ m{(?:\A|/)\.\.(?:/|\z)} or failure('guard');
# The caller fixes /proc in the driver Pod; other roots are private CPU fixtures.
my $uid = $root eq '/proc' ? $> : 0;
scan($root, $uid, "$root/self/mountinfo", "$root/self/uid_map");
`
