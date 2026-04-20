
    for i in 1 2 3 4 5; do
      echo "step $i of 5"
      echo "{\"ts\":\"$(date)\",\"event\":\"progress\",\"pct\":$(echo "$i / 5" | bc -l)}" >> $BUTTONS_PROGRESS_PATH
      sleep 1
    done
    echo "{\"done\":true}"
  