for file in $(ls -1 argocd_*.md)
do
  echo "| Option | Argument type | Description |" >> $file
  echo "| ---------------- | ------ | ---- |" >> $file
  cat $file | grep '   --'| sed -e 's/   */| /g' | sed -e 's/ string/ | string /g' | sed -e 's/ int/ | int /g' | sed -e 's/$/ |/g' >> $file
  sed -i 's/--core| /--core | |/g' $file
  sed -i 's/--grpc-web| /--grpc-web | |/g' $file
  sed -i 's/--port-forward| /--port-forward | |/g' $file
  sed -i 's/--plaintext| /--plaintext | |/g' $file
  sed -i 's/--insecure| /--insecure | |/g' $file
done
